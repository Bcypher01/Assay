package core

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Unit is the unit a Distance is expressed in.
type Unit uint8

const (
	// UnitTicks is a whole number of instrument ticks.
	UnitTicks Unit = iota
	// UnitPips is a number of pips, resolved through the instrument's pip size.
	UnitPips
	// UnitPercent is a percentage of a reference price.
	UnitPercent
	// UnitATR is a multiple of a supplied ATR value.
	UnitATR
	// UnitPrice is an absolute price difference, given in decimal at the input
	// boundary and stored as ticks.
	UnitPrice
	unitCount
)

var unitNames = [unitCount]string{
	UnitTicks:   "ticks",
	UnitPips:    "pips",
	UnitPercent: "percent",
	UnitATR:     "atr",
	UnitPrice:   "price",
}

// ErrUnknownUnit is returned by ParseUnit for an unrecognised string.
var ErrUnknownUnit = fmt.Errorf("core: unknown unit")

// String returns the canonical serialisation form.
func (u Unit) String() string {
	if u >= unitCount {
		return "invalid"
	}
	return unitNames[u]
}

// ParseUnit parses a canonical unit name.
func ParseUnit(s string) (Unit, error) {
	for i, n := range unitNames {
		if n == s {
			return Unit(i), nil
		}
	}
	return 0, fmt.Errorf("%w: %q", ErrUnknownUnit, s)
}

// Distance is a magnitude that carries its unit.
//
// A bare number for a stop buffer or a tolerance is a bug: 0.2 means nothing
// once a strategy moves from EURUSD to BTCUSDT. Distance{UnitATR, 0.2}
// survives the move.
type Distance struct {
	Unit  Unit
	Value Ratio
}

// MarshalJSON keeps strategy documents readable at the authoring boundary.
// The fixed-point Ratio remains an internal detail; users express a distance
// as a decimal amount together with its unit, for example
// {"unit":"pips","value":20}.
func (d Distance) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Unit  string  `json:"unit"`
		Value float64 `json:"value"`
	}{Unit: d.Unit.String(), Value: d.Value.Float64()})
}

// UnmarshalJSON accepts the human-readable distance form used by strategy
// clients. It deliberately requires an explicit unit so a value can never be
// silently interpreted differently when an instrument changes.
func (d *Distance) UnmarshalJSON(data []byte) error {
	var raw struct {
		Unit  string      `json:"unit"`
		Value json.Number `json:"value"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("distance: decode: %w", err)
	}
	unit, err := ParseUnit(raw.Unit)
	if err != nil {
		return err
	}
	value, err := raw.Value.Float64()
	if err != nil {
		return fmt.Errorf("distance: value: %w", err)
	}
	*d = Distance{Unit: unit, Value: RatioFromFloat(value)}
	return nil
}

// Ticks converts d to a number of ticks.
//
// pipTicks is the instrument's pip size expressed in ticks; ref is the
// reference price for percentage distances; atr is the current ATR expressed
// in ticks. Inputs a unit does not need may be zero.
func (d Distance) Ticks(pipTicks int64, ref Price, atr Price) Price {
	switch d.Unit {
	case UnitTicks, UnitPrice:
		return Price(mulDivRound(int64(d.Value), 1, RatioScale))
	case UnitPips:
		return Price(mulDivRound(int64(d.Value), pipTicks, RatioScale))
	case UnitPercent:
		return Price(mulDivRound(int64(ref), int64(d.Value), RatioScale*100))
	case UnitATR:
		return d.Value.MulPrice(atr)
	default:
		return 0
	}
}

// NeedsRef reports whether resolving d requires a reference price.
func (d Distance) NeedsRef() bool { return d.Unit == UnitPercent }

// NeedsATR reports whether resolving d requires an ATR value.
func (d Distance) NeedsATR() bool { return d.Unit == UnitATR }

// String renders d for diagnostics.
func (d Distance) String() string {
	return fmt.Sprintf("%g%s", d.Value.Float64(), d.Unit)
}
