package bls

import (
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/event"
	"testing"
)

func TestArchiveLinksAndReleaseTime(t *testing.T) {
	index := `<a href="/news.release/archives/cpi_09112024.htm">CPI</a><a href="/news.release/archives/empsit_09062024.htm">NFP</a>`
	if got := archiveLinks(index, "cpi_"); len(got) != 1 || got[0] != "/news.release/archives/cpi_09112024.htm" {
		t.Fatalf("%v", got)
	}
	page := `Transmission of material in this news release is embargoed until 8:30 a.m. (ET) Wednesday, September 11, 2024 CONSUMER PRICE INDEX -- AUGUST 2024`
	got, title, err := releaseTime(page)
	if err != nil {
		t.Fatal(err)
	}
	if got != core.MustParseTime("2024-09-11T12:30:00Z") || title == "" {
		t.Fatalf("%d %q", got, title)
	}
}

func TestReleaseTitleUsesTheRequestedFeedKind(t *testing.T) {
	if got := releaseTitle(event.KindUSNonfarmPayrolls); got != "U.S. Employment Situation release" {
		t.Fatalf("NFP title = %q", got)
	}
	if got := releaseTitle(event.KindUSCPI); got != "U.S. Consumer Price Index release" {
		t.Fatalf("CPI title = %q", got)
	}
}
