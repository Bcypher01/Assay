package sim

import (
	"context"
	"sort"
	"time"

	"github.com/olumide/assay/internal/core"
	"github.com/olumide/assay/internal/instrument"
)

type position struct {
	trade  int
	side   core.Side
	entry  core.Price
	stop   core.Price
	target TargetPlan
	lots   core.Ratio
	opened core.Time
	realP  Outcome
	realO  Outcome
}

// Run turns sparse signals and bars into bounded trade outcomes. Signals are
// observed at a bar close and market orders therefore enter at the next bar's
// open; this prevents a signal from trading information from its own bar.
func Run(ctx context.Context, s *core.Series, fine *core.Series, sig Signals, cfg Config, out *RunResult) error {
	if s == nil {
		return ErrNilSeries
	}
	if err := s.Validate(); err != nil {
		return err
	}
	if fine != nil {
		if err := fine.Validate(); err != nil {
			return err
		}
		if !fine.TF.Finer(s.TF) {
			return ErrFineNotFiner
		}
	}
	if err := validSignals(s.Len(), sig); err != nil {
		return err
	}
	if err := validConfig(&cfg); err != nil {
		return err
	}
	if out == nil {
		return errorsOut()
	}
	resetResult(out, s.Len(), sig.Enter.Count())

	equityP, equityO := cfg.Risk.InitialEquity, cfg.Risk.InitialEquity
	dayStartP, dayStartO := equityP, equityO
	day := core.TFD1.AlignDown(0)
	positions := make([]position, 0, max(1, sig.Enter.Count()))
	nextSignal := 0
	signalBar, hasSignal := sig.Enter.Next(0)
	for bar := 0; bar < s.Len(); bar++ {
		if bar&1023 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		thisDay := core.TFD1.AlignDown(s.Time[bar])
		if bar == 0 || thisDay != day {
			day, dayStartP, dayStartO = thisDay, equityP, equityO
		}

		// Existing positions process exits before a newly activated entry.
		for i := 0; i < len(positions); {
			p := &positions[i]
			if partialTarget(*p) && hit(s, bar, p.side, p.target.Level, cfg.Spec.Gap) && !hit(s, bar, p.side, p.stop, cfg.Spec.Gap) {
				realizePartial(p, s.Time[bar], bar, cfg, fillExit(s, bar, p.side, p.target.Level, cfg.Spec.Gap))
				equityP += p.realP.Net
				equityO += p.realO.Net
				p.realP, p.realO = Outcome{}, Outcome{}
				i++
				continue
			}
			pe, oe, ambiguous, resolved := exitsForBar(s, fine, bar, *p, cfg.Spec.Gap)
			if resolved {
				tr := &out.Trades[p.trade]
				tr.Pessimistic = addOutcome(p.realP, settle(*p, pe, s.Time[bar], bar, cfg))
				tr.Optimistic = addOutcome(p.realO, settle(*p, oe, s.Time[bar], bar, cfg))
				tr.Ambiguous = ambiguous
				if ambiguous {
					out.Unresolved++
				}
				equityP += tr.Pessimistic.Net
				equityO += tr.Optimistic.Net
				positions[i] = positions[len(positions)-1]
				positions = positions[:len(positions)-1]
				continue
			}
			updateTrailing(p, s.High[bar], s.Low[bar])
			i++
		}

		// Signals become eligible on the bar following detection.
		for hasSignal && signalBar < bar {
			idx := nextSignal
			nextSignal++
			signalBar, hasSignal = sig.Enter.Next(signalBar + 1)
			if cfg.Direction != 0 && sig.Side[idx] != cfg.Direction {
				continue
			}
			if cfg.MaxConcurrent > 0 && len(positions) >= cfg.MaxConcurrent {
				continue
			}
			if capped(cfg.DailyLossCap, dayStartP, equityP) || capped(cfg.DailyLossCap, dayStartO, equityO) {
				continue
			}
			order := orderAt(sig, idx)
			fill, ok := entryFill(s, bar, sig.Side[idx], order, cfg.Spec.Gap)
			if !ok {
				continue
			}
			fill = applyEntryCosts(fill, sig.Side[idx], cfg)
			req, lots := sizeFor(equityP, fill, sig.StopLevel[idx], cfg)
			if lots <= 0 {
				continue
			}
			tr := Trade{Side: sig.Side[idx], EntryTime: s.Time[bar], EntryBar: bar, EntryPrice: fill, Stop: sig.StopLevel[idx], Target: sig.TargetPlan[idx].Level, Lots: lots, RequestedLots: req}
			tr.RoundingLoss = absRatio(req - lots)
			out.RoundingLoss += tr.RoundingLoss
			out.Trades = append(out.Trades, tr)
			p := position{trade: len(out.Trades) - 1, side: tr.Side, entry: fill, stop: tr.Stop, target: sig.TargetPlan[idx], lots: lots, opened: s.Time[bar]}
			// An order filled at this bar's open is exposed to the rest of this
			// bar. Deferring this check would incorrectly make same-bar stops and
			// targets impossible.
			pe, oe, ambiguous, resolved := exitsForBar(s, fine, bar, p, cfg.Spec.Gap)
			if resolved {
				tr := &out.Trades[p.trade]
				tr.Pessimistic = addOutcome(p.realP, settle(p, pe, s.Time[bar], bar, cfg))
				tr.Optimistic = addOutcome(p.realO, settle(p, oe, s.Time[bar], bar, cfg))
				tr.Ambiguous = ambiguous
				if ambiguous {
					out.Unresolved++
				}
				equityP += tr.Pessimistic.Net
				equityO += tr.Optimistic.Net
			} else {
				updateTrailing(&p, s.High[bar], s.Low[bar])
				positions = append(positions, p)
			}
		}
		out.Equity = append(out.Equity, equityP)
		out.EquityOpt = append(out.EquityOpt, equityO)
	}
	// Mark-to-market open positions at the final close, preserving continuous equity.
	if s.Len() > 0 {
		for _, p := range positions {
			tr := &out.Trades[p.trade]
			o := settle(p, s.Close[s.Len()-1], s.Time[s.Len()-1], s.Len()-1, cfg)
			tr.Pessimistic, tr.Optimistic = addOutcome(p.realP, o), addOutcome(p.realO, o)
			equityP += tr.Pessimistic.Net
			equityO += tr.Optimistic.Net
		}
		out.Equity[len(out.Equity)-1], out.EquityOpt[len(out.EquityOpt)-1] = equityP, equityO
	}
	return nil
}

func partialTarget(p position) bool {
	return p.target.Level != 0 && p.target.Close > 0 && p.target.Close < core.One
}

func realizePartial(p *position, t core.Time, bar int, cfg Config, exit core.Price) {
	closed := p.target.Close.Mul(p.lots)
	if closed <= 0 || closed >= p.lots {
		return
	}
	part := *p
	part.lots = closed
	o := settle(part, exit, t, bar, cfg)
	p.realP, p.realO = addOutcome(p.realP, o), addOutcome(p.realO, o)
	p.lots -= closed
	if p.target.MoveStopToBreakEven {
		p.stop = p.entry
	}
	p.target.Level = 0 // a target is consumed once; remaining size follows its stop/trail.
}

func addOutcome(a, b Outcome) Outcome {
	if !a.Closed {
		return b
	}
	if !b.Closed {
		return a
	}
	return Outcome{ExitTime: b.ExitTime, ExitBar: b.ExitBar, ExitPrice: b.ExitPrice, Gross: a.Gross + b.Gross, Net: a.Net + b.Net, Costs: a.Costs + b.Costs, Closed: true}
}

func errorsOut() error { return ErrNilResult }
func validSignals(n int, x Signals) error {
	if x.Enter == nil || x.Enter.Len() != n || len(x.Side) != x.Enter.Count() || len(x.StopLevel) != x.Enter.Count() || len(x.TargetPlan) != x.Enter.Count() || (len(x.Orders) != 0 && len(x.Orders) != x.Enter.Count()) {
		return ErrInvalidSignals
	}
	for _, side := range x.Side {
		if !side.Valid() {
			return ErrInvalidSignals
		}
	}
	return nil
}
func validConfig(c *Config) error {
	if c.Direction != 0 && !c.Direction.Valid() {
		return ErrInvalidConfig
	}
	if c.Risk.RiskPerTrade < 0 || c.Risk.FixedLots < 0 || c.DailyLossCap < 0 {
		return ErrInvalidConfig
	}
	return c.Spec.Validate()
}
func resetResult(o *RunResult, bars, trades int) {
	o.Trades = o.Trades[:0]
	if cap(o.Trades) < trades {
		o.Trades = make([]Trade, 0, trades)
	}
	o.Equity = o.Equity[:0]
	if cap(o.Equity) < bars {
		o.Equity = make([]core.Price, 0, bars)
	}
	o.EquityOpt = o.EquityOpt[:0]
	if cap(o.EquityOpt) < bars {
		o.EquityOpt = make([]core.Price, 0, bars)
	}
	o.Unresolved = 0
	o.RoundingLoss = 0
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absRatio(r core.Ratio) core.Price {
	if r < 0 {
		return core.Price(-r)
	}
	return core.Price(r)
}
func orderAt(s Signals, n int) Order {
	if len(s.Orders) == 0 {
		return Order{Type: OrderMarket}
	}
	return s.Orders[n]
}
func capped(cap core.Ratio, start, now core.Price) bool {
	return cap > 0 && start-now >= cap.MulPrice(start.Abs())
}

func entryFill(s *core.Series, i int, side core.Side, o Order, gap instrument.GapPolicy) (core.Price, bool) {
	open, hi, lo := s.Open[i], s.High[i], s.Low[i]
	switch o.Type {
	case OrderMarket:
		return open, true
	case OrderLimit:
		if side == core.SideLong {
			if open <= o.Level {
				if gap == instrument.GapFillAtOpen {
					return open, true
				}
				return o.Level, true
			}
			return o.Level, lo <= o.Level
		}
		if open >= o.Level {
			if gap == instrument.GapFillAtOpen {
				return open, true
			}
			return o.Level, true
		}
		return o.Level, hi >= o.Level
	case OrderStop:
		if side == core.SideLong {
			if open >= o.Level {
				if gap == instrument.GapFillAtOpen {
					return open, true
				}
				return o.Level, true
			}
			return o.Level, hi >= o.Level
		}
		if open <= o.Level {
			if gap == instrument.GapFillAtOpen {
				return open, true
			}
			return o.Level, true
		}
		return o.Level, lo <= o.Level
	}
	return 0, false
}
func exitsForBar(s, f *core.Series, i int, p position, gap instrument.GapPolicy) (core.Price, core.Price, bool, bool) {
	stop, target := p.stop, p.target.Level
	sh, th := hit(s, i, p.side, stop, gap), false
	if target != 0 {
		th = hit(s, i, p.side, target, gap)
	}
	if !sh && !th {
		return 0, 0, false, false
	}
	if sh && !th {
		return fillExit(s, i, p.side, stop, gap), fillExit(s, i, p.side, stop, gap), false, true
	}
	if th && !sh {
		return fillExit(s, i, p.side, target, gap), fillExit(s, i, p.side, target, gap), false, true
	}
	if resolved, first, ok := fineFirst(f, s, i, p); ok {
		if first == stop {
			return resolved, resolved, false, true
		}
		return resolved, resolved, false, true
	}
	return fillExit(s, i, p.side, stop, gap), fillExit(s, i, p.side, target, gap), true, true
}
func hit(s *core.Series, i int, side core.Side, l core.Price, g instrument.GapPolicy) bool {
	if side == core.SideLong {
		return s.Low[i] <= l
	}
	return s.High[i] >= l
}
func fillExit(s *core.Series, i int, side core.Side, l core.Price, g instrument.GapPolicy) core.Price {
	if g == instrument.GapFillAtOpen {
		if side == core.SideLong && s.Open[i] < l {
			return s.Open[i]
		}
		if side == core.SideShort && s.Open[i] > l {
			return s.Open[i]
		}
	}
	return l
}
func fineFirst(f, main *core.Series, i int, p position) (core.Price, core.Price, bool) {
	if f == nil {
		return 0, 0, false
	}
	from, to := main.Time[i], main.TF.CloseOf(main.Time[i])
	lo := sort.Search(len(f.Time), func(j int) bool { return f.Time[j] >= from })
	for ; lo < len(f.Time) && f.Time[lo] < to; lo++ {
		sh := hit(f, lo, p.side, p.stop, instrument.GapFillAtLevel)
		th := p.target.Level != 0 && hit(f, lo, p.side, p.target.Level, instrument.GapFillAtLevel)
		if sh && !th {
			return fillExit(f, lo, p.side, p.stop, instrument.GapFillAtLevel), p.stop, true
		}
		if th && !sh {
			return fillExit(f, lo, p.side, p.target.Level, instrument.GapFillAtLevel), p.target.Level, true
		}
		if sh && th {
			return 0, 0, false
		}
	}
	return 0, 0, false
}
func updateTrailing(p *position, hi, lo core.Price) {
	if p.target.TrailDistance <= 0 {
		return
	}
	if p.side == core.SideLong {
		p.stop = core.Max(p.stop, hi-p.target.TrailDistance)
	} else {
		p.stop = core.Min(p.stop, lo+p.target.TrailDistance)
	}
}
func applyEntryCosts(p core.Price, side core.Side, c Config) core.Price {
	return p + side.Sign()*(core.Price(c.Spec.Costs.SpreadTicks)/2+slippage(c))
}
func slippage(c Config) core.Price {
	if c.Slippage.Enabled && c.Slippage.Mode == instrument.SlippageFixed {
		return c.Slippage.Ticks
	}
	if c.Spec.Costs.SlippageMode == instrument.SlippageFixed {
		return core.Price(c.Spec.Costs.SlippageTicks)
	}
	return 0
}
func sizeFor(eq, entry, stop core.Price, c Config) (core.Ratio, core.Ratio) {
	if stop == entry {
		return 0, 0
	}
	if c.Risk.RiskPerTrade == 0 {
		return c.Risk.FixedLots, c.Risk.FixedLots
	}
	tv := c.Risk.TickValue
	if tv == 0 {
		tv = core.One
	}
	req := core.DivPrice(c.Risk.RiskPerTrade.MulPrice(eq.Abs()), (entry - stop).Abs())
	req = divRatio(req, tv)
	step := lotRatio(c.Spec.LotStep)
	if step <= 0 {
		return req, req
	}
	lots := core.Ratio(int64(req) / int64(step) * int64(step))
	if lots < lotRatio(c.Spec.MinLot) {
		return req, 0
	}
	return req, lots
}

func divRatio(a, b core.Ratio) core.Ratio {
	if b == 0 {
		return 0
	}
	// Values here are lots (normally single digits), so the fixed-point
	// product cannot approach int64 limits in a valid instrument run.
	return core.Ratio((int64(a) * core.RatioScale) / int64(b))
}
func lotRatio(s string) core.Ratio {
	if s == "" {
		return 0
	}
	var whole, frac int64
	var digits int
	dot := false
	for _, r := range s {
		if r == '.' {
			dot = true
			continue
		}
		if r < '0' || r > '9' {
			return 0
		}
		if dot {
			frac = frac*10 + int64(r-'0')
			digits++
		} else {
			whole = whole*10 + int64(r-'0')
		}
	}
	for digits < 9 {
		frac *= 10
		digits++
	}
	for digits > 9 {
		frac /= 10
		digits--
	}
	return core.Ratio(whole*core.RatioScale + frac)
}
func settle(p position, exit core.Price, t core.Time, bar int, c Config) Outcome {
	gross := p.side.Sign() * (exit - p.entry)
	gross = core.Ratio(p.lots).MulPrice(gross)
	costs := commission(p, exit, c) + swap(p, t, c)
	return Outcome{ExitTime: t, ExitBar: bar, ExitPrice: exit, Gross: gross, Costs: costs, Net: gross - costs, Closed: true}
}
func commission(p position, exit core.Price, c Config) core.Price {
	cm := c.Spec.Costs
	switch cm.CommissionMode {
	case instrument.CommissionPerTrade:
		return core.Price(cm.CommissionFlat)
	case instrument.CommissionBps:
		return p.lots.MulPrice(cm.CommissionBps.MulPrice(exit.Abs()))
	}
	return 0
}
func swap(p position, exit core.Time, c Config) core.Price {
	days := int((exit - p.opened) / (24 * 60 * 60 * 1000))
	if days <= 0 {
		return 0
	}
	r := c.Spec.Costs.SwapLong
	if p.side == core.SideShort {
		r = c.Spec.Costs.SwapShort
	}
	total := core.Price(0)
	for d := 0; d < days; d++ {
		mult := int64(1)
		if c.Spec.Costs.SwapTripleWeekday >= 0 && p.opened.Std().AddDate(0, 0, d).Weekday() == time.Weekday(c.Spec.Costs.SwapTripleWeekday) {
			mult = 3
		}
		total += r.MulPrice(p.lots.MulPrice(core.Price(mult)))
	}
	return total
}
