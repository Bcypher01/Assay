package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"connectrpc.com/connect"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/primitive"
	"github.com/Bcypher01/Assay/internal/runstore"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"github.com/Bcypher01/Assay/proto/assay/v1/assayv1connect"
)

// ErrNotConfigured is returned when the executable has not supplied a backend
// for an RPC. The transport intentionally owns no data or execution policy.
var ErrNotConfigured = errors.New("rpc: backend not configured")

type EnsureDataFunc func(context.Context, *assayv1.EnsureDataRequest, func(*assayv1.Progress) error) error
type DataStatusFunc func(context.Context, *assayv1.EnsureDataRequest) (*assayv1.DataStatusResponse, error)
type EvaluateFunc func(context.Context, *assayv1.EvaluatePrimitiveRequest) (*assayv1.EvaluatePrimitiveResponse, error)

// RunFunc returns the one terminal summary. Updates supplied to send must be
// non-terminal updates, which keeps exactly-once terminal delivery enforceable
// at the transport boundary.
type RunFunc func(context.Context, *assayv1.RunRequest, func(*assayv1.RunUpdate) error) (*assayv1.RunSummary, error)

// Server is a thin ConnectRPC adapter. Application wiring owns data loading,
// engine execution and run history; handlers only translate wire messages.
type Server struct {
	Registry    *primitive.Registry
	Instruments []*assayv1.Instrument

	EnsureDataFn EnsureDataFunc
	DataStatusFn DataStatusFunc
	EvaluateFn   EvaluateFunc
	RunFn        RunFunc
	// RunStore is the production history backend. The function fields remain
	// available for small embedding applications and transport tests.
	RunStore   runstore.Store
	ListRunsFn func(context.Context) ([]string, error)
	GetRunFn   func(context.Context, string) (*assayv1.RunSummary, error)
	CompareFn  func(context.Context, string, string) (*assayv1.CompareRunsResponse, error)
}

func NewHandler(s *Server) http.Handler {
	if s == nil {
		s = &Server{}
	}
	if s.Registry == nil {
		s.Registry = primitive.DefaultRegistry
	}
	// A local Assay installation always has the embedded instrument catalogue.
	// Supplying it here as a defensive default prevents a partially wired server
	// (or a stale launcher) from presenting a successful-but-empty catalogue to
	// the UI, which otherwise looks like a broken instrument picker.
	if len(s.Instruments) == 0 {
		if registry, err := instrument.Default(); err == nil {
			s.Instruments = instrumentsFromRegistry(registry)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/openapi.json", openAPIHandler)
	mux.HandleFunc("/docs", swaggerHandler)
	path, h := assayv1connect.NewDataServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewPrimitiveServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewStudyServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewRunServiceHandler(s)
	mux.Handle(path, h)
	return withEmptyJSON(mux)
}

func instrumentsFromRegistry(registry *instrument.Registry) []*assayv1.Instrument {
	if registry == nil {
		return nil
	}
	items := make([]*assayv1.Instrument, 0, registry.Len())
	for _, symbol := range registry.Symbols() {
		spec, err := registry.Get(symbol)
		if err != nil {
			continue
		}
		items = append(items, &assayv1.Instrument{
			Symbol:      spec.Symbol,
			QualityTier: string(spec.Quality),
			Timeframes:  supportedTimeframes(spec.Quality),
		})
	}
	return items
}

// Swagger UI versions may submit an Empty protobuf message as a POST with no
// body or content type. Connect correctly rejects that as ambiguous; normalize
// only the three Empty-message endpoints so the interactive documentation stays
// usable without weakening typed request validation elsewhere.
func withEmptyJSON(next http.Handler) http.Handler {
	empty := map[string]bool{
		assayv1connect.DataServiceListInstrumentsProcedure:     true,
		assayv1connect.PrimitiveServiceListPrimitivesProcedure: true,
		assayv1connect.RunServiceListRunsProcedure:             true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && empty[r.URL.Path] && r.Header.Get("Content-Type") == "" && r.ContentLength == 0 {
			r.Header.Set("Content-Type", "application/json")
			r.Body = io.NopCloser(strings.NewReader("{}"))
			r.ContentLength = 2
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) DataStatus(ctx context.Context, req *connect.Request[assayv1.EnsureDataRequest]) (*connect.Response[assayv1.DataStatusResponse], error) {
	if s.DataStatusFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	out, err := s.DataStatusFn(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	if out == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rpc: data status returned nil response"))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) ListInstruments(context.Context, *connect.Request[assayv1.Empty]) (*connect.Response[assayv1.ListInstrumentsResponse], error) {
	items := make([]*assayv1.Instrument, 0, len(s.Instruments))
	for _, in := range s.Instruments {
		if in != nil {
			items = append(items, &assayv1.Instrument{Symbol: in.Symbol, QualityTier: in.QualityTier, Timeframes: append([]string(nil), in.Timeframes...)})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Symbol < items[j].Symbol })
	for _, in := range items {
		sort.Strings(in.Timeframes)
	}
	return connect.NewResponse(&assayv1.ListInstrumentsResponse{Instruments: items}), nil
}

func (s *Server) EnsureData(ctx context.Context, req *connect.Request[assayv1.EnsureDataRequest], stream *connect.ServerStream[assayv1.Progress]) error {
	if s.EnsureDataFn == nil {
		return connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	report := func(p *assayv1.Progress) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == nil {
			return nil
		}
		return stream.Send(p)
	}
	if err := s.EnsureDataFn(ctx, req.Msg, report); err != nil {
		return asConnectError(err)
	}
	return ctx.Err()
}

func (s *Server) ListPrimitives(context.Context, *connect.Request[assayv1.Empty]) (*connect.Response[assayv1.ListPrimitivesResponse], error) {
	reg := s.Registry
	if reg == nil {
		reg = primitive.DefaultRegistry
	}
	out := &assayv1.ListPrimitivesResponse{}
	for _, spec := range reg.Specs() {
		out.Primitives = append(out.Primitives, &assayv1.PrimitiveInfo{Pack: spec.Pack, Type: spec.Type, OutputKind: kindName(spec.Out), ParameterSchema: string(spec.Params.FullName()), Timeframed: spec.Timeframed})
	}
	return connect.NewResponse(out), nil
}

func (s *Server) Evaluate(ctx context.Context, req *connect.Request[assayv1.EvaluatePrimitiveRequest]) (*connect.Response[assayv1.EvaluatePrimitiveResponse], error) {
	if s.EvaluateFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	out, err := s.EvaluateFn(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	if out == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rpc: evaluator returned nil response"))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) Validate(_ context.Context, req *connect.Request[assayv1.ValidateRequest]) (*connect.Response[assayv1.ValidateResponse], error) {
	var strategy ir.Strategy
	if err := json.Unmarshal(req.Msg.StrategyJson, &strategy); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("decode strategy: %w", err))
	}
	reg := s.Registry
	if reg == nil {
		reg = primitive.DefaultRegistry
	}
	out := &assayv1.ValidateResponse{Hash: strategy.Hash()}
	for _, e := range strategy.Validate(reg) {
		out.Errors = append(out.Errors, &assayv1.FieldError{Path: e.Path, Message: e.Message})
	}
	return connect.NewResponse(out), nil
}

func (s *Server) Run(ctx context.Context, req *connect.Request[assayv1.RunRequest], stream *connect.ServerStream[assayv1.RunUpdate]) error {
	if err := sendUpdate(ctx, stream, progressUpdate(0, "validating")); err != nil {
		return err
	}
	validated, err := s.Validate(ctx, connect.NewRequest(&assayv1.ValidateRequest{StrategyJson: req.Msg.StrategyJson}))
	if err != nil {
		return sendTerminal(ctx, stream, &assayv1.RunSummary{Error: err.Error()})
	}
	if len(validated.Msg.Errors) != 0 {
		return sendTerminal(ctx, stream, &assayv1.RunSummary{Error: validationFailure(validated.Msg.Errors)})
	}
	if s.RunFn == nil {
		return sendTerminal(ctx, stream, &assayv1.RunSummary{Error: ErrNotConfigured.Error()})
	}
	send := func(update *assayv1.RunUpdate) error {
		if update == nil || update.GetSummary() != nil {
			return fmt.Errorf("rpc: runner may not send terminal updates")
		}
		return sendUpdate(ctx, stream, update)
	}
	summary, err := s.RunFn(ctx, req.Msg, send)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return sendTerminal(ctx, stream, &assayv1.RunSummary{Error: err.Error()})
	}
	if summary == nil {
		return sendTerminal(ctx, stream, &assayv1.RunSummary{Error: "rpc: runner returned nil summary"})
	}
	return sendTerminal(ctx, stream, summary)
}

// validationFailure keeps a streamed run useful when the caller skipped the
// separate Validate endpoint: surface the first few actionable field errors,
// rather than making the UI send the user hunting for a generic failure.
func validationFailure(errors []*assayv1.FieldError) string {
	const maxShown = 3
	parts := make([]string, 0, min(len(errors), maxShown))
	for _, fieldErr := range errors {
		if fieldErr == nil || fieldErr.Message == "" {
			continue
		}
		if fieldErr.Path == "" {
			parts = append(parts, fieldErr.Message)
		} else {
			parts = append(parts, fieldErr.Path+": "+fieldErr.Message)
		}
		if len(parts) == maxShown {
			break
		}
	}
	if len(parts) == 0 {
		return "Strategy validation failed. Open Strategy Editor to review the setup."
	}
	remaining := len(errors) - len(parts)
	message := "Strategy needs attention: " + strings.Join(parts, "; ")
	if remaining > 0 {
		message += fmt.Sprintf("; and %d more", remaining)
	}
	return message + ". Open Strategy Editor to fix it."
}

func (s *Server) ListRuns(ctx context.Context, _ *connect.Request[assayv1.Empty]) (*connect.Response[assayv1.ListRunsResponse], error) {
	if s.ListRunsFn == nil && s.RunStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	var hashes []string
	var err error
	if s.ListRunsFn != nil {
		hashes, err = s.ListRunsFn(ctx)
	} else {
		var runs []runstore.RunSummary
		runs, err = s.RunStore.List(ctx, runstore.Filter{})
		for _, run := range runs {
			hashes = append(hashes, string(run.ID))
		}
	}
	if err != nil {
		return nil, asConnectError(err)
	}
	sort.Strings(hashes)
	return connect.NewResponse(&assayv1.ListRunsResponse{Hashes: hashes}), nil
}
func (s *Server) GetRun(ctx context.Context, req *connect.Request[assayv1.GetRunRequest]) (*connect.Response[assayv1.RunSummary], error) {
	if s.GetRunFn == nil && s.RunStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	var out *assayv1.RunSummary
	var err error
	if s.GetRunFn != nil {
		out, err = s.GetRunFn(ctx, req.Msg.Hash)
	} else {
		var run *runstore.Run
		run, err = s.RunStore.Get(ctx, runstore.RunID(req.Msg.Hash))
		if run != nil {
			out = &assayv1.RunSummary{Trades: uint32(run.RunSummary.Trades)}
		}
	}
	if err != nil {
		return nil, asConnectError(err)
	}
	if out == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("run %q not found", req.Msg.Hash))
	}
	return connect.NewResponse(out), nil
}
func (s *Server) Compare(ctx context.Context, req *connect.Request[assayv1.CompareRunsRequest]) (*connect.Response[assayv1.CompareRunsResponse], error) {
	if s.CompareFn == nil && s.RunStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	var out *assayv1.CompareRunsResponse
	var err error
	if s.CompareFn != nil {
		out, err = s.CompareFn(ctx, req.Msg.Left, req.Msg.Right)
	} else {
		var diff runstore.Diff
		diff, err = s.RunStore.Compare(ctx, runstore.RunID(req.Msg.Left), runstore.RunID(req.Msg.Right))
		out = &assayv1.CompareRunsResponse{}
		for _, change := range diff.ParamChanges {
			out.Changes = append(out.Changes, &assayv1.ParamChange{Path: change.Path, From: change.From, To: change.To})
		}
		for name, delta := range diff.MetricDeltas {
			out.MetricDeltas = append(out.MetricDeltas, &assayv1.MetricDelta{Name: name, Pessimistic: delta.Pessimistic, Optimistic: delta.Optimistic})
		}
		sort.Slice(out.MetricDeltas, func(i, j int) bool { return out.MetricDeltas[i].Name < out.MetricDeltas[j].Name })
	}
	if err != nil {
		return nil, asConnectError(err)
	}
	if out == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rpc: comparer returned nil response"))
	}
	return connect.NewResponse(out), nil
}

func progressUpdate(percent uint32, stage string) *assayv1.RunUpdate {
	return &assayv1.RunUpdate{Payload: &assayv1.RunUpdate_Progress{Progress: &assayv1.Progress{Percent: percent, Stage: stage, StageCode: progressStageCode(stage)}}}
}

func progressStageCode(stage string) assayv1.ProgressStage {
	switch stage {
	case "checking local data":
		return assayv1.ProgressStage_PROGRESS_STAGE_CHECKING_LOCAL_DATA
	case "data already available", "ready":
		return assayv1.ProgressStage_PROGRESS_STAGE_READY
	case "downloading":
		return assayv1.ProgressStage_PROGRESS_STAGE_DOWNLOADING
	case "verifying":
		return assayv1.ProgressStage_PROGRESS_STAGE_VERIFYING
	case "saving locally", "preparing data":
		return assayv1.ProgressStage_PROGRESS_STAGE_SAVING_LOCALLY
	case "loading data", "loading cache":
		return assayv1.ProgressStage_PROGRESS_STAGE_LOADING_CACHE
	case "evaluating strategy":
		return assayv1.ProgressStage_PROGRESS_STAGE_EVALUATING_STRATEGY
	case "streaming results":
		return assayv1.ProgressStage_PROGRESS_STAGE_CALCULATING_METRICS
	default:
		return assayv1.ProgressStage_PROGRESS_STAGE_UNSPECIFIED
	}
}
func sendUpdate(ctx context.Context, stream *connect.ServerStream[assayv1.RunUpdate], update *assayv1.RunUpdate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return stream.Send(update)
}
func sendTerminal(ctx context.Context, stream *connect.ServerStream[assayv1.RunUpdate], summary *assayv1.RunSummary) error {
	return sendUpdate(ctx, stream, &assayv1.RunUpdate{Payload: &assayv1.RunUpdate_Summary{Summary: summary}})
}
func asConnectError(err error) error {
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	return err
}

func kindName(k primitive.Kind) string {
	switch k {
	case primitive.KindBool:
		return "bool"
	case primitive.KindLevel:
		return "level"
	case primitive.KindZone:
		return "zone"
	case primitive.KindNumber:
		return "number"
	case primitive.KindState:
		return "state"
	default:
		return "unknown"
	}
}
