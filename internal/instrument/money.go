package instrument

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/Bcypher01/Assay/internal/core"
)

// AccountCurrency is the currency in which the built-in simulator reports
// equity, P&L, costs, and user-entered balances. Values are integer cents.
const AccountCurrency = "USD"

// SupportsUSDCashAccounting reports whether this spec has enough information
// to value its profit and loss in USD. USDT is treated as USD at par; this is
// explicit because it is an assumption, not a claim that a stablecoin is risk
// free. USDJPY converts JPY proceeds through its closing USDJPY price.
func (s Spec) SupportsUSDCashAccounting() bool {
	q, settle := strings.ToUpper(s.QuoteCurrency), strings.ToUpper(s.SettleCurrency)
	// Unit-test and embedded callers that construct a minimal Spec retain the
	// historical one-tick-is-one-cent convention. Registered instruments must
	// always declare real currencies and contract size.
	if q == "" && s.ContractSize == "" {
		return true
	}
	return q == "USD" || q == "USDT" || (q == "JPY" && settle == "USD" && strings.HasPrefix(strings.ToUpper(s.Symbol), "DUKAS:USD"))
}

// PnLCents converts a price change in instrument ticks at lots into USD cents.
// conversionPrice is the closing price and is only used for USDJPY, where JPY
// quote-currency P&L is converted back to USD. It rounds once, at the account
// minor-unit boundary, preserving sub-cent precision during the calculation.
func (s Spec) PnLCents(ticks core.Price, lots core.Ratio, conversionPrice core.Price) (core.Price, error) {
	if !s.SupportsUSDCashAccounting() {
		return 0, fmt.Errorf("instrument: %s cannot be valued in %s", s.Symbol, AccountCurrency)
	}
	tickSize, contractSize := s.TickSize, s.ContractSize
	if tickSize == "" {
		tickSize = "1"
	}
	if contractSize == "" {
		contractSize = "1"
	}
	tickMant, tickFrac, err := parseDecimal(tickSize)
	if err != nil {
		return 0, err
	}
	contractMant, contractFrac, err := parseDecimal(contractSize)
	if err != nil {
		return 0, err
	}
	n := big.NewInt(int64(ticks))
	n.Mul(n, big.NewInt(tickMant))
	n.Mul(n, big.NewInt(contractMant))
	n.Mul(n, big.NewInt(int64(lots)))
	n.Mul(n, big.NewInt(100)) // USD cents
	d := big.NewInt(core.RatioScale)
	d.Mul(d, pow10Big(tickFrac))
	d.Mul(d, pow10Big(contractFrac))
	if strings.ToUpper(s.QuoteCurrency) == "JPY" {
		if conversionPrice <= 0 {
			return 0, fmt.Errorf("instrument: %s requires a positive USDJPY conversion price", s.Symbol)
		}
		// conversionPrice is itself in ticks. Cancel its tick-size scale
		// against the quote amount before dividing JPY by JPY/USD.
		n.Mul(n, pow10Big(tickFrac))
		d.Mul(d, big.NewInt(int64(conversionPrice)))
		d.Mul(d, big.NewInt(tickMant))
	}
	return core.Price(roundRat(n, d)), nil
}

// QuoteMinorCents converts a quote-currency minor-unit fee into USD cents.
// USD and USDT costs are already cents; USDJPY fees are whole JPY.
func (s Spec) QuoteMinorCents(minor int64, conversionPrice core.Price) (core.Price, error) {
	q := strings.ToUpper(s.QuoteCurrency)
	if q == "USD" || q == "USDT" || q == "" {
		return core.Price(minor), nil
	}
	if q != "JPY" || conversionPrice <= 0 {
		return 0, fmt.Errorf("instrument: %s cannot convert quote minor units to %s", s.Symbol, AccountCurrency)
	}
	tickMant, tickFrac, err := parseDecimal(s.TickSize)
	if err != nil {
		return 0, err
	}
	n := big.NewInt(minor)
	n.Mul(n, big.NewInt(100))
	n.Mul(n, pow10Big(tickFrac))
	d := big.NewInt(int64(conversionPrice))
	d.Mul(d, big.NewInt(tickMant))
	return core.Price(roundRat(n, d)), nil
}

func pow10Big(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

func roundRat(n, d *big.Int) int64 {
	neg := n.Sign() < 0
	a := new(big.Int).Abs(n)
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(a, d, r)
	if r.Lsh(r, 1).Cmp(d) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if neg {
		q.Neg(q)
	}
	return q.Int64()
}
