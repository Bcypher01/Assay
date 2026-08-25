package rpc

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/Bcypher01/Assay/internal/core"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"github.com/Bcypher01/Assay/proto/assay/v1/assayv1connect"
)

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
