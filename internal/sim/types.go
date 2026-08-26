package sim

import (
	"errors"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
)

var (
	ErrNilSeries      = errors.New("sim: nil series")
	ErrNilResult      = errors.New("sim: nil result")
	ErrInvalidSignals = errors.New("sim: invalid signals")
	ErrInvalidConfig  = errors.New("sim: invalid config")
	ErrFineNotFiner   = errors.New("sim: fine series is not finer than main series")
)

// OrderType selects the condition which activates an entry order.
type OrderType uint8

const (
	OrderMarket OrderType = iota
	OrderLimit
	OrderStop
)

// Order is an optional entry instruction. Zero Order is a market order. Order
// entries are aligned to set bits in Signals.Enter, just like Side and StopLevel.
type Order struct {
	Type  OrderType
	Level core.Price // ignored for market orders
}

// TargetPlan describes the protective and profit-taking levels for one entry.
// A zero target means the position remains open until its stop or the final bar.
type TargetPlan struct {
	Level core.Price
	// Close is the fraction closed at the target. Zero means all remaining size.
	Close core.Ratio
	// MoveStopToBreakEven moves the remaining stop to the entry fill after target.
	MoveStopToBreakEven bool
	// TrailDistance trails the remaining stop by this many ticks. Zero disables it.
	TrailDistance core.Price
}

// Signals are sparse entry instructions. Side, StopLevel, TargetPlan and Orders
// are aligned to Enter's set bits, not bar indexes. StopDistance and
// TargetDistance are optional fixed tick offsets resolved from the actual entry
// fill. Stop/TargetDistanceSpec keep percentage and ATR distances honest by
// resolving them after the market order fill is known.
type Signals struct {
	Enter          *core.Bitset
	Side           []core.Side
	StopLevel      []core.Price
	TargetPlan     []TargetPlan
	Orders         []Order
	StopDistance   []core.Price
	TargetDistance []core.Price
	StopSpec       []core.Distance
	TargetSpec     []core.Distance
}

// RiskModel controls position size. Equity, PnL, and costs are USD cents.
// Instrument contract and conversion rules determine the tick value.
type RiskModel struct {
	InitialEquity core.Price
	RiskPerTrade  core.Ratio // fraction of current pessimistic equity
	FixedLots     core.Ratio // used when RiskPerTrade is zero
}

// SlippageModel overrides the spec slippage when Enabled. It is deliberately
// explicit: the default execution model remains the instrument's non-zero cost.
type SlippageModel struct {
	Enabled bool
	Mode    instrument.SlippageMode
	Ticks   core.Price
	ATR     core.Ratio
}

// Config controls one deterministic simulation.
type Config struct {
	Spec          instrument.Spec
	Risk          RiskModel
	MaxConcurrent int        // zero or negative means unlimited
	DailyLossCap  core.Ratio // fraction of day-start equity; zero disables
	Slippage      SlippageModel
	Direction     core.Side // zero allows both directions
}

// Outcome is one possible result of a trade. Pessimistic and Optimistic only
// differ when an unresolved bar can touch competing exit levels.
type Outcome struct {
	ExitTime  core.Time
	ExitBar   int
	ExitPrice core.Price
	Gross     core.Price
	Net       core.Price
	Costs     core.Price
	Closed    bool
}

// Trade records the common entry facts and the two bounded outcomes.
type Trade struct {
	Side          core.Side
	EntryTime     core.Time
	EntryBar      int
	EntryPrice    core.Price
	Stop          core.Price
	Target        core.Price
	Lots          core.Ratio
	RequestedLots core.Ratio
	RoundingLoss  core.Price
	Pessimistic   Outcome
	Optimistic    Outcome
	Ambiguous     bool
}

// RunResult is safe to reuse: Run resets its lengths and preserves capacities.
type RunResult struct {
	Trades       []Trade
	Equity       []core.Price
	EquityOpt    []core.Price
	Unresolved   int
	RoundingLoss core.Price
}
