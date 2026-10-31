package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/gen/go/assay/v1/assayv1connect"
	"github.com/bcypher01/assay/internal/analytics"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/runstore"
	"github.com/bcypher01/assay/internal/setuptest"
	"github.com/bcypher01/assay/internal/sim"
)

func TestSetupJSONBoundary(t *testing.T) {
	handler := NewHandler(&Server{SetupChartFn: func(_ context.Context, req setuptest.ChartRequest) (setuptest.Chart, error) {
		return setuptest.Chart{Symbol: req.Symbol, Timeframe: req.Timeframe, Snapshot: "fixed", Bars: []setuptest.Bar{{TimeMS: 1, Open: 10, High: 12, Low: 9, Close: 11}}}, nil
	}})

	// The server serves no interface. Root advertises the API entry points and
	// nothing else; a redirect to an embedded page here would be a second copy
	// of the UI waiting to diverge from the real one.
	rootResponse := httptest.NewRecorder()
	handler.ServeHTTP(rootResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if rootResponse.Code != http.StatusOK || !strings.Contains(rootResponse.Body.String(), `"service":"assay"`) {
		t.Fatalf("root status=%d body=%q", rootResponse.Code, rootResponse.Body.String())
	}
	uiResponse := httptest.NewRecorder()
	handler.ServeHTTP(uiResponse, httptest.NewRequest(http.MethodGet, "/setup/", nil))
	if uiResponse.Code != http.StatusNotFound {
		t.Fatalf("embedded setup UI is still mounted: status=%d", uiResponse.Code)
	}

	body, err := json.Marshal(setuptest.ChartRequest{Symbol: "DUKAS:EURUSD", Timeframe: "d1", FromMS: 1, ToMS: 2})
	if err != nil {
		t.Fatal(err)
	}
	apiRequest := httptest.NewRequest(http.MethodPost, "/api/setup/chart", bytes.NewReader(body))
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Code != http.StatusOK || !strings.Contains(apiResponse.Body.String(), `"snapshot":"fixed"`) {
		t.Fatalf("setup API status=%d body=%q", apiResponse.Code, apiResponse.Body.String())
	}
}

func TestSetupBoundaryRejectsUnknownFields(t *testing.T) {
	handler := NewHandler(&Server{SetupChartFn: func(context.Context, setuptest.ChartRequest) (setuptest.Chart, error) {
		return setuptest.Chart{}, nil
	}})
	request := httptest.NewRequest(http.MethodPost, "/api/setup/chart", strings.NewReader(`{"symbol":"DUKAS:EURUSD","unknown":true}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "unknown field") {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestOpenAPIAndSwaggerEndpoints(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&Server{}))
	defer srv.Close()
	for _, path := range []string{"/openapi.json", "/docs"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", path, res.StatusCode)
		}
	}
}

func TestConnectJSONUnaryRequest(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&Server{}))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/assay.v1.DataService/ListInstruments", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestHandlerHasEmbeddedInstrumentCatalogueByDefault(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&Server{}))
	defer srv.Close()
	data := assayv1connect.NewDataServiceClient(srv.Client(), srv.URL)
	res, err := data.ListInstruments(context.Background(), connect.NewRequest(&assayv1.Empty{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Msg.Instruments) == 0 {
		t.Fatal("default handler returned no instruments")
	}
	if res.Msg.Instruments[0].Symbol == "" {
		t.Fatal("default handler returned a blank instrument symbol")
	}
}

func TestTemplateCatalogueAndDescriptionRoundTrip(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&Server{}))
	defer srv.Close()
	templates := assayv1connect.NewTemplateServiceClient(srv.Client(), srv.URL)
	listed, err := templates.ListTemplates(context.Background(), connect.NewRequest(&assayv1.Empty{}))
	if err != nil {
		t.Fatal(err)
	}
	// The catalogue is whatever the engine offers; pinning a number here only
	// records how many templates existed the day the test was written.
	if want := len(ir.PopularTemplates()); len(listed.Msg.Templates) != want {
		t.Fatalf("templates = %d, want %d", len(listed.Msg.Templates), want)
	}
	first := listed.Msg.Templates[0]
	if first.Id != "moving-average-crossover" || first.StrategyDescription.GetSummary() == "" || len(first.StrategyJson) == 0 {
		t.Fatalf("first template = %#v", first)
	}
	study := assayv1connect.NewStudyServiceClient(srv.Client(), srv.URL)
	description, err := study.Describe(context.Background(), connect.NewRequest(&assayv1.DescribeStrategyRequest{StrategyJson: first.StrategyJson}))
	if err != nil {
		t.Fatal(err)
	}
	if description.Msg.Description.GetSummary() != first.StrategyDescription.GetSummary() || len(description.Msg.Description.Rules) != 2 {
		t.Fatalf("description = %#v", description.Msg.Description)
	}
}

func TestSwaggerStyleEmptyRequestIsNormalized(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&Server{}))
	defer srv.Close()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/assay.v1.DataService/ListInstruments", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

func TestServicesRoundTripAndRunHasOneTerminalSummary(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listeners unavailable: %v", err)
	}
	srv := httptest.NewUnstartedServer(NewHandler(&Server{
		Instruments: []*assayv1.Instrument{{Symbol: "Z", Timeframes: []string{"H1", "M5"}}, {Symbol: "A"}},
		EnsureDataFn: func(_ context.Context, _ *assayv1.EnsureDataRequest, report func(*assayv1.Progress) error) error {
			if err := report(&assayv1.Progress{Percent: 20, Stage: "download"}); err != nil {
				return err
			}
			return report(&assayv1.Progress{Percent: 100, Stage: "complete"})
		},
	}))
	srv.Listener = listener
	srv.Start()
	defer srv.Close()
	data := assayv1connect.NewDataServiceClient(srv.Client(), srv.URL)
	listed, err := data.ListInstruments(context.Background(), connect.NewRequest(&assayv1.Empty{}))
	if err != nil {
		t.Fatal(err)
	}
	if got := listed.Msg.Instruments[0].Symbol; got != "A" {
		t.Fatalf("first symbol = %q", got)
	}
	if got := listed.Msg.Instruments[1].Timeframes[0]; got != "H1" {
		t.Fatalf("first timeframe = %q", got)
	}
	progress, err := data.EnsureData(context.Background(), connect.NewRequest(&assayv1.EnsureDataRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for progress.Receive() {
		count++
	}
	if err := progress.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("progress count = %d, want 2", count)
	}

	study := assayv1connect.NewStudyServiceClient(srv.Client(), srv.URL)
	run, err := study.Run(context.Background(), connect.NewRequest(&assayv1.RunRequest{StrategyJson: []byte("{")}))
	if err != nil {
		t.Fatal(err)
	}
	var updates, terminal int
	for run.Receive() {
		updates++
		if run.Msg().GetSummary() != nil {
			terminal++
		}
	}
	if err := run.Err(); err != nil {
		t.Fatal(err)
	}
	if updates != 2 || terminal != 1 {
		t.Fatalf("updates=%d terminal=%d, want 2 and 1", updates, terminal)
	}
}

func TestValidationFailureMakesRunErrorsActionable(t *testing.T) {
	got := validationFailure([]*assayv1.FieldError{
		{Path: "instrument", Message: "is required"},
		{Path: "nodes.0", Message: "unknown primitive"},
		{Path: "risk.stop", Message: "must be positive"},
		{Path: "slots.target", Message: "is required"},
	})
	for _, want := range []string{
		"instrument: is required",
		"nodes.0: unknown primitive",
		"risk.stop: must be positive",
		"and 1 more",
		"Open Strategy Editor to fix it.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("validation failure %q does not contain %q", got, want)
		}
	}
}

func TestDownsampleViewportPreservesBucketExtremes(t *testing.T) {
	s := core.NewSeries("X", core.TFM1, 10)
	for i := 0; i < 10; i++ {
		s.Append(core.Time(i*60_000), 10, 11, 9, 10, 1)
	}
	s.High[3], s.Low[7] = 99, -50
	points := DownsampleViewport(s, 6)
	if len(points) > 6 {
		t.Fatalf("point count = %d", len(points))
	}
	if points[0].Time != s.Time[0] || points[len(points)-1].Time != s.Time[9] {
		t.Fatal("endpoints were not retained")
	}
	var high, low bool
	for _, p := range points {
		high = high || p.High == 99
		low = low || p.Low == -50
	}
	if !high || !low {
		t.Fatalf("extremes retained: high=%v low=%v", high, low)
	}
}

func TestRunDetailsAndPaginatedTradesExposePersistedEvidence(t *testing.T) {
	history, err := runstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	study := &analytics.Study{Strategy: &ir.Strategy{Name: "evidence", Universe: ir.Universe{Symbols: []string{"EURUSD"}}}, Snapshot: "snapshot", Scope: analytics.Scope{Instruments: []string{"EURUSD"}, Period: "2024"}, Assumptions: analytics.Assumptions{InitialEquity: 100_000, SpreadTicks: 2, FillPolicy: "next_open"}, MonteCarlo: analytics.MCConfig{Samples: 3, BlockLength: 2, Seed: 7}}
	result := &analytics.StudyResult{FullPeriod: analytics.Metrics{Trades: 3, Wins: 2, Losses: 1, FinalEquity: analytics.Bounded{Pessimistic: 100_030, Optimistic: 100_060}}, OutOfSample: analytics.Metrics{Trades: 2, Wins: 1, Losses: 1, FinalEquity: analytics.Bounded{Pessimistic: 100_010, Optimistic: 100_020}, Expectancy: analytics.Bounded{Pessimistic: 5, Optimistic: 10}, ProfitFactor: analytics.Bounded{Pessimistic: 1.5, Optimistic: 2}}, InSample: analytics.Metrics{Trades: 2}, NearMisses: []analytics.NearMiss{{Condition: "trigger", Count: 7}}, Unresolved: 1, Unfilled: 3, Distribution: analytics.MCDistribution{FinalEquity: []float64{1, 2, 3}, MaxDrawdown: []float64{2, 4, 6}, RealisedFinal: 2, RealisedDrawdown: 4}, WalkForward: []analytics.WalkForwardResult{{Window: analytics.WFWindow{TrainTo: 10, TestFrom: 10, TestTo: 15}, TrainFromMs: 1_000, TrainToMs: 2_000, TestFromMs: 3_000, TestToMs: 4_000, Metrics: analytics.Metrics{Trades: 1}}}, CostStress: []analytics.CostStressPoint{{Multiplier: 1, Metrics: analytics.Metrics{Trades: 2}}, {Multiplier: 3, Metrics: analytics.Metrics{Trades: 2}}}, Sensitivity: analytics.Grid{X: []string{"1× costs"}, Y: []string{"1× risk"}, Cells: [][]analytics.Metrics{{{Trades: 2}}}}, NextActions: []analytics.NextAction{{Kind: "paper_trade", Reason: "stable"}}}
	id, err := history.Put(context.Background(), study, result)
	if err != nil {
		t.Fatal(err)
	}
	trades := []sim.Trade{
		{Side: core.SideLong, EntryTime: core.MustParseTime("2024-01-02T00:00:00Z"), EntryPrice: 10, Pessimistic: sim.Outcome{ExitTime: core.MustParseTime("2024-01-03T00:00:00Z"), ExitPrice: 12, Net: 5, Costs: 1}, Optimistic: sim.Outcome{ExitTime: core.MustParseTime("2024-01-03T00:00:00Z"), ExitPrice: 13, Net: 8, Costs: 1}},
		{Side: core.SideShort, EntryTime: core.MustParseTime("2024-02-02T00:00:00Z"), EntryPrice: 20, Pessimistic: sim.Outcome{Net: -3, Costs: 2}, Optimistic: sim.Outcome{Net: 2, Costs: 2}, Ambiguous: true},
	}
	if err := history.PutTrades(context.Background(), id, trades); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(&Server{RunStore: history}))
	defer srv.Close()
	runs := assayv1connect.NewRunServiceClient(srv.Client(), srv.URL)
	details, err := runs.GetDetails(context.Background(), connect.NewRequest(&assayv1.GetRunRequest{Hash: string(id)}))
	if err != nil {
		t.Fatal(err)
	}
	got := details.Msg.Details
	if got.GetAssumptions().GetInitialEquity() != 100_000 || got.GetUnresolved() != 1 || len(got.GetMonthly()) != 2 || got.GetNearMisses()[0].GetCount() != 7 {
		t.Fatalf("details = %#v", got)
	}
	if got.GetMonteCarlo().GetSamples() != 3 || got.GetMonteCarlo().GetFinalEquity().GetP50() != 2 || len(got.GetWalkForward()) != 1 || len(got.GetCostStress()) != 2 || len(got.GetSensitivity().GetCells()) != 1 || got.GetNextActions()[0].GetKind() != "paper_trade" {
		t.Fatalf("robustness = %#v", got)
	}
	// Full period and out of sample are separate samples, not two labels on the
	// same numbers: the result page must be able to tell them apart.
	if got.GetFullPeriod().GetTrades() != 3 || got.GetOutOfSample().GetTrades() != 2 || got.GetInSample().GetTrades() != 2 {
		t.Fatalf("evidence samples = full %d, out %d, in %d", got.GetFullPeriod().GetTrades(), got.GetOutOfSample().GetTrades(), got.GetInSample().GetTrades())
	}
	if wf := got.GetWalkForward()[0]; wf.GetTestFromMs() != 3_000 || wf.GetTestToMs() != 4_000 || wf.GetTrainFromMs() != 1_000 {
		t.Fatalf("walk-forward dates = %#v", wf)
	}
	summary, err := runs.GetRun(context.Background(), connect.NewRequest(&assayv1.GetRunRequest{Hash: string(id)}))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Msg.GetRunId() != string(id) || summary.Msg.GetTrades() != 2 || summary.Msg.GetWins() != 1 || summary.Msg.GetLosses() != 1 || summary.Msg.GetUnresolved() != 1 || summary.Msg.GetUnfilled() != 3 || summary.Msg.GetNetPnl() != 10 || summary.Msg.GetExpectancy() != 5 || summary.Msg.GetProfitFactor() != 1.5 {
		t.Fatalf("summary = %#v", summary.Msg)
	}
	page, err := runs.GetTrades(context.Background(), connect.NewRequest(&assayv1.GetRunTradesRequest{Hash: string(id), PageSize: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if page.Msg.GetTotal() != 2 || len(page.Msg.GetTrades()) != 1 || page.Msg.GetNextOffset() != 1 || page.Msg.GetTrades()[0].GetCosts().GetPessimistic() != 1 {
		t.Fatalf("trades page = %#v", page.Msg)
	}
}

func TestPooledWindowReturnsCountsEachTradeOnce(t *testing.T) {
	trades := []sim.Trade{
		{EntryBar: 0, Pessimistic: sim.Outcome{Net: 10}, Optimistic: sim.Outcome{Net: 20}},
		{EntryBar: 5, Pessimistic: sim.Outcome{Net: -4}, Optimistic: sim.Outcome{Net: 6}},
		{EntryBar: 10, Pessimistic: sim.Outcome{Net: 7}, Optimistic: sim.Outcome{Net: 7}},
	}
	// The first two spans overlap; a trade in the overlap must still be counted
	// once. The third trade sits outside every span.
	got := pooledWindowReturns(trades, [][2]int{{0, 8}, {4, 8}, {12, 20}})
	if len(got.Pessimistic) != 2 || got.Pessimistic[0] != 10 || got.Pessimistic[1] != -4 {
		t.Fatalf("pooled returns = %#v", got)
	}
	if got.Optimistic[0] != 20 || got.Optimistic[1] != 6 {
		t.Fatalf("pooled optimistic returns = %#v", got)
	}
}
