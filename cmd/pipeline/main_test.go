package main

import (
	"testing"

	"github.com/bcypher01/assay/internal/instrument"
)

func TestPublishableSymbols(t *testing.T) {
	reg := instrument.MustDefault()
	in := []string{"BINANCE:BTCUSDT", "DUKAS:EURUSD", "BINANCE:ETHUSDT"}

	// A hosted (R2) library withholds unlicensed FX instead of failing the run.
	got, withheld := publishableSymbols(reg, in, "r2", false)
	if len(got) != 2 || got[0] != "BINANCE:BTCUSDT" || got[1] != "BINANCE:ETHUSDT" {
		t.Fatalf("publishable = %v", got)
	}
	if len(withheld) != 1 || withheld[0] != "DUKAS:EURUSD" {
		t.Fatalf("withheld = %v", withheld)
	}

	// Confirmed rights publish everything.
	all, none := publishableSymbols(reg, in, "r2", true)
	if len(all) != 3 || len(none) != 0 {
		t.Fatalf("authorized = %v, withheld = %v", all, none)
	}

	// The local filesystem publisher is a development path, not distribution.
	local, noneLocal := publishableSymbols(reg, in, "local", false)
	if len(local) != 3 || len(noneLocal) != 0 {
		t.Fatalf("local = %v, withheld = %v", local, noneLocal)
	}
}
