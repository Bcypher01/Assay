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
	"time"

	"connectrpc.com/connect"
	"github.com/Bcypher01/Assay/internal/analytics"
	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/onboarding"
	"github.com/Bcypher01/Assay/internal/primitive"
	"github.com/Bcypher01/Assay/internal/runstore"
	"github.com/Bcypher01/Assay/internal/setuptest"
	"github.com/Bcypher01/Assay/internal/setupui"
	"github.com/Bcypher01/Assay/internal/sim"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"github.com/Bcypher01/Assay/proto/assay/v1/assayv1connect"
)

// ErrNotConfigured is returned when the executable has not supplied a backend
// for an RPC. The transport intentionally owns no data or execution policy.
var ErrNotConfigured = errors.New("rpc: backend not configured")

type EnsureDataFunc func(context.Context, *assayv1.EnsureDataRequest, func(*assayv1.Progress) error) error
type DataStatusFunc func(context.Context, *assayv1.EnsureDataRequest) (*assayv1.DataStatusResponse, error)
type EvaluateFunc func(context.Context, *assayv1.EvaluatePrimitiveRequest) (*assayv1.EvaluatePrimitiveResponse, error)
type QueryEventsFunc func(context.Context, *assayv1.QueryEventsRequest) (*assayv1.QueryEventsResponse, error)
type RunEventStudyFunc func(context.Context, *assayv1.EventStudyRequest) (*assayv1.EventStudyResponse, error)
type ListEventSnapshotsFunc func(context.Context) ([]event.SnapshotMetadata, error)
type SetupChartFunc func(context.Context, setuptest.ChartRequest) (setuptest.Chart, error)
type SetupPreviewFunc func(context.Context, setuptest.PreviewRequest) (setuptest.Preview, error)
type SetupRunFunc func(context.Context, setuptest.RunRequest) (*assayv1.RunSummary, error)

// RunFunc returns the one terminal summary. Updates supplied to send must be
// non-terminal updates, which keeps exactly-once terminal delivery enforceable
// at the transport boundary.
type RunFunc func(context.Context, *assayv1.RunRequest, func(*assayv1.RunUpdate) error) (*assayv1.RunSummary, error)

// EventRecorder receives aggregate, local-only product metrics when the
// launcher has explicitly enabled it. Recorder failures never affect a study.
type EventRecorder interface {
	Record(context.Context, string, map[string]any) error
}

// Server is a thin ConnectRPC adapter. Application wiring owns data loading,
// engine execution and run history; handlers only translate wire messages.
type Server struct {
	Registry    *primitive.Registry
	Instruments []*assayv1.Instrument

	EnsureDataFn         EnsureDataFunc
	DataStatusFn         DataStatusFunc
	EvaluateFn           EvaluateFunc
	QueryEventsFn        QueryEventsFunc
	RunEventStudyFn      RunEventStudyFunc
	ListEventSnapshotsFn ListEventSnapshotsFunc
	RunFn                RunFunc
	SetupChartFn         SetupChartFunc
	SetupPreviewFn       SetupPreviewFunc
	SetupRunFn           SetupRunFunc
	// RunStore is the production history backend. The function fields remain
	// available for small embedding applications and transport tests.
	RunStore   runstore.Store
	ListRunsFn func(context.Context) ([]string, error)
	GetRunFn   func(context.Context, string) (*assayv1.RunSummary, error)
	CompareFn  func(context.Context, string, string) (*assayv1.CompareRunsResponse, error)
	Recorder   EventRecorder
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
	mux.HandleFunc("/onboarding.json", onboardingHandler)
	mux.HandleFunc("/event-snapshots", s.eventSnapshotsHandler)
	mux.HandleFunc("/openapi.json", openAPIHandler)
	mux.HandleFunc("/docs", swaggerHandler)
	mux.HandleFunc("/api/setup/chart", s.setupChartHandler)
	mux.HandleFunc("/api/setup/preview", s.setupPreviewHandler)
	mux.HandleFunc("/api/setup/run", s.setupRunHandler)
	mux.Handle("/setup/", http.StripPrefix("/setup/", setupui.Handler()))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/setup/", http.StatusTemporaryRedirect)
	})
	path, h := assayv1connect.NewDataServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewEventStudyServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewPrimitiveServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewStudyServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewTemplateServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewRunServiceHandler(s)
	mux.Handle(path, h)
	return withEmptyJSON(mux)
}

func (s *Server) setupChartHandler(w http.ResponseWriter, r *http.Request) {
	if s.SetupChartFn == nil {
		writeSetupError(w, ErrNotConfigured, http.StatusNotImplemented)
		return
	}
	var req setuptest.ChartRequest
	if !decodeSetupRequest(w, r, &req) {
		return
	}
	out, err := s.SetupChartFn(r.Context(), req)
	if err != nil {
		writeSetupError(w, err, http.StatusBadRequest)
		return
	}
	writeSetupJSON(w, out)
}

func (s *Server) setupPreviewHandler(w http.ResponseWriter, r *http.Request) {
	if s.SetupPreviewFn == nil {
		writeSetupError(w, ErrNotConfigured, http.StatusNotImplemented)
		return
	}
	var req setuptest.PreviewRequest
	if !decodeSetupRequest(w, r, &req) {
		return
	}
	out, err := s.SetupPreviewFn(r.Context(), req)
	if err != nil {
		writeSetupError(w, err, http.StatusBadRequest)
		return
	}
	writeSetupJSON(w, out)
}

func (s *Server) setupRunHandler(w http.ResponseWriter, r *http.Request) {
	if s.SetupRunFn == nil {
		writeSetupError(w, ErrNotConfigured, http.StatusNotImplemented)
		return
	}
	var req setuptest.RunRequest
	if !decodeSetupRequest(w, r, &req) {
		return
	}
	out, err := s.SetupRunFn(r.Context(), req)
	if err != nil {
		writeSetupError(w, err, http.StatusBadRequest)
		return
	}
	writeSetupJSON(w, out)
}

func decodeSetupRequest(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeSetupError(w, fmt.Errorf("method not allowed"), http.StatusMethodNotAllowed)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeSetupError(w, fmt.Errorf("decode setup request: %w", err), http.StatusBadRequest)
		return false
	}
	return true
}

func writeSetupError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: err.Error()})
}

func writeSetupJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}

// eventSnapshotsHandler is deliberately ordinary read-only JSON: the static
// client can populate its event-snapshot picker before it constructs a
// ConnectRPC client. The detailed event records remain available only through
// QueryEvents, which requires an explicit content hash.
func (s *Server) eventSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.ListEventSnapshotsFn == nil {
		http.Error(w, ErrNotConfigured.Error(), http.StatusNotImplemented)
		return
	}
	items, err := s.ListEventSnapshotsFn(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		Snapshots []event.SnapshotMetadata `json:"snapshots"`
	}{Snapshots: items})
}

func (s *Server) QueryEvents(ctx context.Context, req *connect.Request[assayv1.QueryEventsRequest]) (*connect.Response[assayv1.QueryEventsResponse], error) {
	if s.QueryEventsFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	out, err := s.QueryEventsFn(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	if out == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rpc: event query returned nil response"))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) RunEventStudy(ctx context.Context, req *connect.Request[assayv1.EventStudyRequest]) (*connect.Response[assayv1.EventStudyResponse], error) {
	if s.RunEventStudyFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	out, err := s.RunEventStudyFn(ctx, req.Msg)
	if err != nil {
		return nil, asConnectError(err)
	}
	if out == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rpc: event study returned nil response"))
	}
	return connect.NewResponse(out), nil
}

// onboardingHandler is intentionally a small ordinary JSON document so a
// static UI can load a useful first-run state before its ConnectRPC client is
// initialised. It contains only an example template and public data window.
func onboardingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(onboarding.Default())
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
		assayv1connect.TemplateServiceListTemplatesProcedure:   true,
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

// Describe exposes the generated reading of the persisted strategy document so
// the UI cannot substitute display-only text for executable rules.
func (s *Server) Describe(_ context.Context, req *connect.Request[assayv1.DescribeStrategyRequest]) (*connect.Response[assayv1.DescribeStrategyResponse], error) {
	var strategy ir.Strategy
	if err := json.Unmarshal(req.Msg.StrategyJson, &strategy); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("decode strategy: %w", err))
	}
	return connect.NewResponse(&assayv1.DescribeStrategyResponse{Description: strategyDescription(ir.Describe(&strategy))}), nil
}

func (s *Server) ListTemplates(context.Context, *connect.Request[assayv1.Empty]) (*connect.Response[assayv1.ListTemplatesResponse], error) {
	out := &assayv1.ListTemplatesResponse{}
	for _, definition := range ir.PopularTemplates() {
		strategy := definition.New()
		encoded, err := json.Marshal(strategy)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("encode template %s: %w", definition.ID, err))
		}
		out.Templates = append(out.Templates, &assayv1.StrategyTemplate{
			Id: definition.ID, Version: definition.Version, Name: definition.Name,
			Family: definition.Family, Description: definition.Description,
			ExpectedFrequency: definition.ExpectedFrequency, StrategyJson: encoded,
			StrategyDescription: strategyDescription(ir.Describe(strategy)),
		})
	}
	return connect.NewResponse(out), nil
}

func strategyDescription(description ir.Description) *assayv1.StrategyDescription {
	out := &assayv1.StrategyDescription{Summary: description.Summary}
	for _, rule := range description.Rules {
		out.Rules = append(out.Rules, &assayv1.StrategyRule{Branch: rule.Branch, Text: rule.Text, Enabled: rule.Enabled})
	}
	return out
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

// GetDetails exposes the persisted evidence for a completed run while keeping
// GetRun's compact streaming-era summary compatible with existing clients.
func (s *Server) GetDetails(ctx context.Context, req *connect.Request[assayv1.GetRunRequest]) (*connect.Response[assayv1.GetRunDetailsResponse], error) {
	if s.RunStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	run, err := s.RunStore.Get(ctx, runstore.RunID(req.Msg.Hash))
	if err != nil {
		return nil, asConnectError(err)
	}
	trials, err := s.RunStore.Trials(ctx, run.Scope)
	if err != nil {
		return nil, asConnectError(err)
	}
	d := &assayv1.RunDetails{RunId: string(run.ID), StrategyHash: run.StrategyHash, Snapshot: run.Snapshot, Name: run.Name, Period: run.Scope.Period, Instruments: append([]string(nil), run.Scope.Instruments...), OutOfSample: resultMetrics(run.Result.OutOfSample), InSample: resultMetrics(run.Result.InSample), Trials: uint32(trials), TradeListPresent: run.TradeListPresent, Assumptions: assumptionsMessage(run.Study.Assumptions), Unresolved: uint32(run.Result.Unresolved), Yearly: periodMetrics(run.Trades, "2006"), Monthly: periodMetrics(run.Trades, "2006-01")}
	for _, miss := range run.Result.NearMisses {
		d.NearMisses = append(d.NearMisses, &assayv1.NearMissSummary{Condition: miss.Condition, Count: uint32(miss.Count)})
	}
	for _, window := range run.Result.WalkForward {
		d.WalkForward = append(d.WalkForward, &assayv1.WalkForwardWindow{TrainFrom: uint32(window.Window.TrainFrom), TrainTo: uint32(window.Window.TrainTo), TestFrom: uint32(window.Window.TestFrom), TestTo: uint32(window.Window.TestTo), OutOfSample: resultMetrics(window.Metrics)})
	}
	for _, point := range run.Result.CostStress {
		d.CostStress = append(d.CostStress, &assayv1.CostStressPoint{Multiplier: uint32(point.Multiplier), Metrics: resultMetrics(point.Metrics)})
	}
	d.MonteCarlo = monteCarloMessage(run.Result.Distribution, run.Study.MonteCarlo, stableStudySeed(run.Study))
	d.Sensitivity = sensitivityMessage(run.Result.Sensitivity)
	for _, action := range run.Result.NextActions {
		d.NextActions = append(d.NextActions, &assayv1.NextAction{Kind: action.Kind, Reason: action.Reason})
	}
	return connect.NewResponse(&assayv1.GetRunDetailsResponse{Details: d}), nil
}

// GetTrades supplies a bounded, page-addressable trade inspector. A result
// page must never load an entire retained parquet list merely to render its
// first viewport.
func (s *Server) GetTrades(ctx context.Context, req *connect.Request[assayv1.GetRunTradesRequest]) (*connect.Response[assayv1.GetRunTradesResponse], error) {
	if s.RunStore == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	run, err := s.RunStore.Get(ctx, runstore.RunID(req.Msg.Hash))
	if err != nil {
		return nil, asConnectError(err)
	}
	if !run.TradeListPresent {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("run %q no longer has its retained trade list", req.Msg.Hash))
	}
	page := int(req.Msg.PageSize)
	if page == 0 {
		page = 100
	}
	if page > 500 {
		page = 500
	}
	from := int(req.Msg.PageOffset)
	if from < 0 || from > len(run.Trades) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("page_offset is outside the trade list"))
	}
	to := from + page
	if to > len(run.Trades) {
		to = len(run.Trades)
	}
	out := &assayv1.GetRunTradesResponse{Total: uint32(len(run.Trades))}
	for _, trade := range run.Trades[from:to] {
		out.Trades = append(out.Trades, tradeRecord(trade))
	}
	if to < len(run.Trades) {
		out.NextOffset = uint32(to)
	}
	return connect.NewResponse(out), nil
}

func bounded(v analytics.Bounded) *assayv1.BoundedMetric {
	return &assayv1.BoundedMetric{Pessimistic: v.Pessimistic, Optimistic: v.Optimistic}
}
func resultMetrics(x analytics.Metrics) *assayv1.ResultMetrics {
	return &assayv1.ResultMetrics{FinalEquity: bounded(x.FinalEquity), MaxDrawdown: bounded(x.MaxDrawdown), AverageDrawdown: bounded(x.AverageDrawdown), ProfitFactor: bounded(x.ProfitFactor), Expectancy: bounded(x.Expectancy), Trades: uint32(x.Trades), Wins: uint32(x.Wins), Losses: uint32(x.Losses), LongestLosingStreak: uint32(x.LongestLosingStreak)}
}
func assumptionsMessage(a analytics.Assumptions) *assayv1.StudyAssumptions {
	return &assayv1.StudyAssumptions{InitialEquity: a.InitialEquity, RiskPerTrade: a.RiskPerTrade, FixedLots: a.FixedLots, SpreadTicks: a.SpreadTicks, CommissionBps: a.CommissionBps, CommissionFlat: a.CommissionFlat, SlippageTicks: a.SlippageTicks, FillPolicy: a.FillPolicy}
}

func tradeRecord(t sim.Trade) *assayv1.TradeRecord {
	side := "long"
	if t.Side == core.SideShort {
		side = "short"
	}
	return &assayv1.TradeRecord{Side: side, EntryTimeMs: t.EntryTime.Millis(), ExitTimeMsPessimistic: t.Pessimistic.ExitTime.Millis(), ExitTimeMsOptimistic: t.Optimistic.ExitTime.Millis(), EntryPrice: int64(t.EntryPrice), ExitPricePessimistic: int64(t.Pessimistic.ExitPrice), ExitPriceOptimistic: int64(t.Optimistic.ExitPrice), Stop: int64(t.Stop), Target: int64(t.Target), Lots: int64(t.Lots), NetPnl: &assayv1.BoundedMetric{Pessimistic: float64(t.Pessimistic.Net), Optimistic: float64(t.Optimistic.Net)}, Costs: &assayv1.BoundedMetric{Pessimistic: float64(t.Pessimistic.Costs), Optimistic: float64(t.Optimistic.Costs)}, Ambiguous: t.Ambiguous}
}

func periodMetrics(trades []sim.Trade, layout string) []*assayv1.PeriodMetrics {
	type aggregate struct {
		period               string
		p, o                 float64
		trades, wins, losses uint32
	}
	items := make([]aggregate, 0)
	for _, trade := range trades {
		period := time.UnixMilli(trade.EntryTime.Millis()).UTC().Format(layout)
		i := len(items) - 1
		if i < 0 || items[i].period != period {
			items = append(items, aggregate{period: period})
			i++
		}
		x := &items[i]
		x.p += float64(trade.Pessimistic.Net)
		x.o += float64(trade.Optimistic.Net)
		x.trades++
		if trade.Pessimistic.Net > 0 {
			x.wins++
		} else if trade.Pessimistic.Net < 0 {
			x.losses++
		}
	}
	out := make([]*assayv1.PeriodMetrics, len(items))
	for i, x := range items {
		out[i] = &assayv1.PeriodMetrics{Period: x.period, NetPnl: &assayv1.BoundedMetric{Pessimistic: x.p, Optimistic: x.o}, Trades: x.trades, Wins: x.wins, Losses: x.losses}
	}
	return out
}

func monteCarloMessage(d analytics.MCDistribution, c analytics.MCConfig, seed uint64) *assayv1.MonteCarloSummary {
	if c.Samples == 0 {
		c.Samples, c.BlockLength, c.Seed = 1000, 5, seed
	}
	return &assayv1.MonteCarloSummary{FinalEquity: percentiles(d.FinalEquity), MaxDrawdown: percentiles(d.MaxDrawdown), RealisedFinalEquity: d.RealisedFinal, RealisedMaxDrawdown: d.RealisedDrawdown, Samples: uint32(c.Samples), BlockLength: uint32(c.BlockLength), Seed: c.Seed}
}
func percentiles(v []float64) *assayv1.Percentile {
	if len(v) == 0 {
		return &assayv1.Percentile{}
	}
	at := func(p float64) float64 { return v[int(p*float64(len(v)-1))] }
	return &assayv1.Percentile{P05: at(.05), P50: at(.5), P95: at(.95)}
}
func stableStudySeed(s *analytics.Study) uint64 {
	if s != nil && s.Strategy != nil {
		var h uint64 = 14695981039346656037
		for _, b := range []byte(s.Strategy.Hash()) {
			h ^= uint64(b)
			h *= 1099511628211
		}
		return h
	}
	return 0
}
func sensitivityMessage(g analytics.Grid) *assayv1.SensitivityGrid {
	out := &assayv1.SensitivityGrid{X: append([]string(nil), g.X...), Y: append([]string(nil), g.Y...)}
	for _, row := range g.Cells {
		for _, cell := range row {
			out.Cells = append(out.Cells, &assayv1.SensitivityCell{Metrics: resultMetrics(cell)})
		}
	}
	return out
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
