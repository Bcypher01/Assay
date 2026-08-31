package instrument

import (
	"testing"

	"github.com/bcypher01/assay/internal/core"
)

func TestPnLCentsForSupportedInstruments(t *testing.T) {
	cases := []struct {
		name, symbol, tick, contract, quote, settle string
		ticks, conversion                           core.Price
		lots                                        core.Ratio
		want                                        core.Price
	}{
		{"EURUSD full lot", "DUKAS:EURUSD", "0.00001", "100000", "USD", "USD", 1, 0, core.One, 100},
		{"EURUSD micro lot", "DUKAS:EURUSD", "0.00001", "100000", "USD", "USD", 1, 0, core.RatioFromFloat(.01), 1},
		{"BTCUSDT", "BINANCE:BTCUSDT", "0.01", "1", "USDT", "USDT", 1, 0, core.One, 1},
		{"USDJPY", "DUKAS:USDJPY", "0.001", "100000", "JPY", "USD", 1, 150000, core.One, 67},
		{"USDCAD", "DUKAS:USDCAD", "0.00001", "100000", "CAD", "USD", 1, 135000, core.One, 74},
		{"USDCHF", "DUKAS:USDCHF", "0.00001", "100000", "CHF", "USD", 1, 88000, core.One, 114},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Spec{Symbol: tc.symbol, TickSize: tc.tick, ContractSize: tc.contract, QuoteCurrency: tc.quote, SettleCurrency: tc.settle}
			got, err := s.PnLCents(tc.ticks, tc.lots, tc.conversion)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("PnLCents = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestEveryRegisteredInstrumentSupportsUSDCashAccounting(t *testing.T) {
	r := MustDefault()
	for _, symbol := range r.Symbols() {
		s, err := r.Get(symbol)
		if err != nil {
			t.Fatal(err)
		}
		if !s.SupportsUSDCashAccounting() {
			t.Fatalf("%s is missing USD account valuation", symbol)
		}
	}
}

func TestQuoteMinorCentsForUSDJPY(t *testing.T) {
	s := Spec{Symbol: "DUKAS:USDJPY", TickSize: "0.001", ContractSize: "100000", QuoteCurrency: "JPY", SettleCurrency: "USD"}
	got, err := s.QuoteMinorCents(100, 150_000)
	if err != nil {
		t.Fatal(err)
	}
	if got != 67 {
		t.Fatalf("100 JPY fee = %d cents, want 67", got)
	}
}

func TestQuoteMinorCentsForUSDCAD(t *testing.T) {
	s := Spec{Symbol: "DUKAS:USDCAD", TickSize: "0.00001", ContractSize: "100000", QuoteCurrency: "CAD", SettleCurrency: "USD"}
	got, err := s.QuoteMinorCents(100, 135_000)
	if err != nil {
		t.Fatal(err)
	}
	if got != 74 {
		t.Fatalf("CAD 1 fee = %d cents, want 74", got)
	}
}
