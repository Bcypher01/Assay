package pipeline

import "testing"

func TestURIEncodePathUsesSigV4Encoding(t *testing.T) {
	got := uriEncodePath("/assay/DUKAS:EURUSD/h1/2020-01.manifest.json")
	want := "/assay/DUKAS%3AEURUSD/h1/2020-01.manifest.json"
	if got != want {
		t.Fatalf("canonical path = %q, want %q", got, want)
	}
}

func TestURIEncodePathDoesNotDoubleEncodeDecodedPath(t *testing.T) {
	got := uriEncodePath("/assay/a b/100%/café")
	want := "/assay/a%20b/100%25/caf%C3%A9"
	if got != want {
		t.Fatalf("canonical path = %q, want %q", got, want)
	}
}
