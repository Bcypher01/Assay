package instrument

import "github.com/bcypher01/assay/internal/core"

// SpreadMode selects how spread is applied.
type SpreadMode uint8

const (
	// SpreadFixed applies a constant spread in ticks.
	SpreadFixed SpreadMode = iota
	// SpreadFromData uses the bid/ask spread recorded in the data where
	// available, falling back to Fixed where it is not.
	SpreadFromData
)

// CommissionMode selects how commission is charged.
type CommissionMode uint8

const (
	// CommissionNone charges nothing.
	CommissionNone CommissionMode = iota
	// CommissionPerTrade charges a flat amount per trade, in quote-currency
	// minor units.
	CommissionPerTrade
	// CommissionBps charges a fraction of notional, in basis points.
	CommissionBps
)

// SlippageMode selects how slippage is modelled.
type SlippageMode uint8

const (
	// SlippageFixed applies a constant number of ticks against the trade.
	SlippageFixed SlippageMode = iota
	// SlippageATR applies a fraction of current ATR against the trade.
	SlippageATR
)

// CostModel describes what execution costs on this instrument.
//
// Slippage defaults to non-zero in every shipped spec. A zero default is a lie
// that most users will never notice and never change.
type CostModel struct {
	SpreadMode  SpreadMode `json:"spreadMode"`
	SpreadTicks int64      `json:"spreadTicks"`

	CommissionMode CommissionMode `json:"commissionMode"`
	CommissionBps  core.Ratio     `json:"commissionBps"`
	CommissionFlat int64          `json:"commissionFlat"`

	SlippageMode  SlippageMode `json:"slippageMode"`
	SlippageTicks int64        `json:"slippageTicks"`
	SlippageATR   core.Ratio   `json:"slippageAtr"`

	// SwapLong and SwapShort accrue per calendar day held, in ticks per unit.
	SwapLong  core.Ratio `json:"swapLong"`
	SwapShort core.Ratio `json:"swapShort"`
	// SwapTripleWeekday is the weekday on which triple swap is charged, using
	// time.Weekday numbering. Negative disables it.
	SwapTripleWeekday int `json:"swapTripleWeekday"`
}

// Validate reports whether the model is internally consistent.
func (c CostModel) Validate() error {
	if c.SpreadTicks < 0 {
		return errField("costs.spreadTicks", "must not be negative")
	}
	if c.SlippageTicks < 0 {
		return errField("costs.slippageTicks", "must not be negative")
	}
	if c.CommissionBps < 0 || c.CommissionFlat < 0 {
		return errField("costs.commission", "must not be negative")
	}
	if c.SwapTripleWeekday > 6 {
		return errField("costs.swapTripleWeekday", "must be 0-6, or negative to disable")
	}
	return nil
}
