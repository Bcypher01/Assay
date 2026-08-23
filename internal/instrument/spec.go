package instrument

import (
	"fmt"

	"github.com/Bcypher01/Assay/internal/core"
)

// AssetClass groups instruments by the conventions they follow.
type AssetClass string

const (
	ClassCrypto    AssetClass = "crypto"
	ClassFX        AssetClass = "fx"
	ClassMetal     AssetClass = "metal"
	ClassIndex     AssetClass = "index"
	ClassCommodity AssetClass = "commodity"
	ClassEquity    AssetClass = "equity"
)

// QualityTier is the finest resolution available for an instrument. Results
// computed at different tiers are not comparable, and the engine refuses a
// strategy whose precision requirement exceeds the tier.
type QualityTier string

const (
	QualityTick   QualityTier = "tick"
	QualityS1     QualityTier = "s1"
	QualityM1     QualityTier = "m1"
	QualityHourly QualityTier = "hourly"
	QualityDaily  QualityTier = "daily"
)

// FinestTF returns the finest timeframe the tier supports.
func (q QualityTier) FinestTF() core.TF {
	switch q {
	case QualityTick:
		return core.TFTick
	case QualityS1:
		return core.TFS1
	case QualityM1:
		return core.TFM1
	case QualityHourly:
		return core.TFH1
	default:
		return core.TFD1
	}
}

// Supports reports whether the tier can honestly serve the given timeframe.
func (q QualityTier) Supports(tf core.TF) bool {
	f := q.FinestTF()
	return tf == f || tf.Coarser(f)
}

// GapPolicy selects how orders behave across a price gap.
type GapPolicy string

const (
	// GapFillAtOpen fills a gapped-through order at the opening price of the
	// bar that gapped, which is what a real broker does.
	GapFillAtOpen GapPolicy = "fill_at_open"
	// GapFillAtLevel fills at the requested level regardless of the gap. Only
	// honest for instruments that genuinely do not gap.
	GapFillAtLevel GapPolicy = "fill_at_level"
)

// Spec describes one tradeable instrument.
//
// TickSize is expressed as a decimal string rather than a float so that the
// spec itself is exact: "0.00001" cannot be represented in binary floating
// point, and a spec that is approximate makes every price derived from it
// approximate too.
type Spec struct {
	Symbol  string     `json:"symbol"`
	Class   AssetClass `json:"class"`
	Display string     `json:"display"`
	Venue   string     `json:"venue"`

	TickSize     string `json:"tickSize"`
	PipTicks     int64  `json:"pipTicks"`
	ContractSize string `json:"contractSize"`
	LotStep      string `json:"lotStep"`
	MinLot       string `json:"minLot"`

	QuoteCurrency  string `json:"quoteCurrency"`
	SettleCurrency string `json:"settleCurrency"`

	Costs    CostModel   `json:"costs"`
	Calendar *Calendar   `json:"calendar"`
	Gap      GapPolicy   `json:"gap"`
	Quality  QualityTier `json:"quality"`
	Source   string      `json:"source"`

	tickMant  int64 // tick size mantissa
	tickFrac  int   // tick size fractional digits
	priceFrac int   // digits used when rendering a price
}

// PriceFromDecimal converts a decimal price string to ticks, exactly.
// It returns an error when the value is not an exact multiple of the tick
// size — silently rounding venue data would corrupt every bar built from it.
func (s *Spec) PriceFromDecimal(v string) (core.Price, error) {
	mant, frac, err := parseDecimal(v)
	if err != nil {
		return 0, err
	}
	// Bring both to a common scale.
	scale := frac
	if s.tickFrac > scale {
		scale = s.tickFrac
	}
	vm := mant * pow10(scale-frac)
	tm := s.tickMant * pow10(scale-s.tickFrac)
	if tm == 0 {
		return 0, errField("tickSize", "must be positive")
	}
	if vm%tm != 0 {
		return 0, fmt.Errorf("instrument: price %s is not a multiple of tick size %s for %s", v, s.TickSize, s.Symbol)
	}
	return core.Price(vm / tm), nil
}

// MustPriceFromDecimal is PriceFromDecimal for fixtures and tests.
func (s *Spec) MustPriceFromDecimal(v string) core.Price {
	p, err := s.PriceFromDecimal(v)
	if err != nil {
		panic(err)
	}
	return p
}

// PriceToDecimal renders a price back to its decimal form. Round-tripping
// through PriceFromDecimal is exact.
func (s *Spec) PriceToDecimal(p core.Price) string {
	mant := int64(p) * s.tickMant
	return formatDecimal(mant, s.tickFrac)
}

// PriceDigits returns the number of fractional digits prices are quoted to.
func (s *Spec) PriceDigits() int { return s.priceFrac }

// Resolve converts a Distance to ticks using this instrument's conventions.
func (s *Spec) Resolve(d core.Distance, ref, atr core.Price) core.Price {
	if d.Unit == core.UnitPrice {
		// A price-unit distance arrives already converted to ticks by the
		// input boundary; there is no decimal left to interpret here.
		return d.Value.MulPrice(1)
	}
	return d.Ticks(s.PipTicks, ref, atr)
}

// Validate checks the spec and prepares its derived fields.
func (s *Spec) Validate() error {
	if s.Symbol == "" {
		return errField("symbol", "must not be empty")
	}
	mant, frac, err := parseDecimal(s.TickSize)
	if err != nil {
		return errField("tickSize", err.Error())
	}
	if mant <= 0 {
		return errField("tickSize", "must be positive")
	}
	s.tickMant, s.tickFrac = mant, frac
	s.priceFrac = frac
	if s.PipTicks <= 0 {
		s.PipTicks = 1
	}
	if s.Gap == "" {
		s.Gap = GapFillAtOpen
	}
	if s.Quality == "" {
		s.Quality = QualityM1
	}
	if err := s.Costs.Validate(); err != nil {
		return err
	}
	if s.Calendar == nil {
		s.Calendar = Continuous()
	} else if err := s.Calendar.init(); err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		val  string
	}{
		{"contractSize", s.ContractSize},
		{"lotStep", s.LotStep},
		{"minLot", s.MinLot},
	} {
		if f.val == "" {
			continue
		}
		if _, _, err := parseDecimal(f.val); err != nil {
			return errField(f.name, err.Error())
		}
	}
	return nil
}

// FieldError names the offending field, so a bad spec points at itself.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("instrument: %s: %s", e.Field, e.Message)
}

func errField(field, msg string) error { return &FieldError{Field: field, Message: msg} }
