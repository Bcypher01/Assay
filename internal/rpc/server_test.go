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
	"github.com/Bcypher01/Assay/internal/analytics"
	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/runstore"
	"github.com/Bcypher01/Assay/internal/setuptest"
	"github.com/Bcypher01/Assay/internal/sim"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"github.com/Bcypher01/Assay/proto/assay/v1/assayv1connect"
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
	if len(listed.Msg.Templates) != 9 {
		t.Fatalf("templates = %d, want 9", len(listed.Msg.Templates))
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
	result := &analytics.StudyResult{OutOfSample: analytics.Metrics{Trades: 2, FinalEquity: analytics.Bounded{Pessimistic: 10, Optimistic: 20}}, InSample: analytics.Metrics{Trades: 2}, NearMisses: []analytics.NearMiss{{Condition: "trigger", Count: 7}}, Unresolved: 1, Distribution: analytics.MCDistribution{FinalEquity: []float64{1, 2, 3}, MaxDrawdown: []float64{2, 4, 6}, RealisedFinal: 2, RealisedDrawdown: 4}, WalkForward: []analytics.WalkForwardResult{{Window: analytics.WFWindow{TrainTo: 10, TestFrom: 10, TestTo: 15}, Metrics: analytics.Metrics{Trades: 1}}}, CostStress: []analytics.CostStressPoint{{Multiplier: 1, Metrics: analytics.Metrics{Trades: 2}}, {Multiplier: 3, Metrics: analytics.Metrics{Trades: 2}}}, Sensitivity: analytics.Grid{X: []string{"1× costs"}, Y: []string{"1× risk"}, Cells: [][]analytics.Metrics{{{Trades: 2}}}}, NextActions: []analytics.NextAction{{Kind: "paper_trade", Reason: "stable"}}}
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
	page, err := runs.GetTrades(context.Background(), connect.NewRequest(&assayv1.GetRunTradesRequest{Hash: string(id), PageSize: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if page.Msg.GetTotal() != 2 || len(page.Msg.GetTrades()) != 1 || page.Msg.GetNextOffset() != 1 || page.Msg.GetTrades()[0].GetCosts().GetPessimistic() != 1 {
		t.Fatalf("trades page = %#v", page.Msg)
	}
}
