package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"connectrpc.com/connect"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/primitive"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"github.com/Bcypher01/Assay/proto/assay/v1/assayv1connect"
)

// ErrNotConfigured is returned when the executable has not supplied a backend
// for an RPC. The transport intentionally owns no data or execution policy.
var ErrNotConfigured = errors.New("rpc: backend not configured")

type EnsureDataFunc func(context.Context, *assayv1.EnsureDataRequest, func(*assayv1.Progress) error) error
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
	EvaluateFn   EvaluateFunc
	RunFn        RunFunc
	ListRunsFn   func(context.Context) ([]string, error)
	GetRunFn     func(context.Context, string) (*assayv1.RunSummary, error)
	CompareFn    func(context.Context, string, string) (*assayv1.CompareRunsResponse, error)
}

func NewHandler(s *Server) http.Handler {
	if s == nil {
		s = &Server{}
	}
	if s.Registry == nil {
		s.Registry = primitive.DefaultRegistry
	}
	mux := http.NewServeMux()
	path, h := assayv1connect.NewDataServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewPrimitiveServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewStudyServiceHandler(s)
	mux.Handle(path, h)
	path, h = assayv1connect.NewRunServiceHandler(s)
	mux.Handle(path, h)
	return mux
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
		return sendTerminal(ctx, stream, &assayv1.RunSummary{Error: "strategy validation failed"})
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

func (s *Server) ListRuns(ctx context.Context, _ *connect.Request[assayv1.Empty]) (*connect.Response[assayv1.ListRunsResponse], error) {
	if s.ListRunsFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	hashes, err := s.ListRunsFn(ctx)
	if err != nil {
		return nil, asConnectError(err)
	}
	sort.Strings(hashes)
	return connect.NewResponse(&assayv1.ListRunsResponse{Hashes: hashes}), nil
}
func (s *Server) GetRun(ctx context.Context, req *connect.Request[assayv1.GetRunRequest]) (*connect.Response[assayv1.RunSummary], error) {
	if s.GetRunFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	out, err := s.GetRunFn(ctx, req.Msg.Hash)
	if err != nil {
		return nil, asConnectError(err)
	}
	if out == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("run %q not found", req.Msg.Hash))
	}
	return connect.NewResponse(out), nil
}
func (s *Server) Compare(ctx context.Context, req *connect.Request[assayv1.CompareRunsRequest]) (*connect.Response[assayv1.CompareRunsResponse], error) {
	if s.CompareFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, ErrNotConfigured)
	}
	out, err := s.CompareFn(ctx, req.Msg.Left, req.Msg.Right)
	if err != nil {
		return nil, asConnectError(err)
	}
	if out == nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rpc: comparer returned nil response"))
	}
	return connect.NewResponse(out), nil
}

func progressUpdate(percent uint32, stage string) *assayv1.RunUpdate {
	return &assayv1.RunUpdate{Payload: &assayv1.RunUpdate_Progress{Progress: &assayv1.Progress{Percent: percent, Stage: stage}}}
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
