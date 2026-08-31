package eventstudy

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
	"github.com/Bcypher01/Assay/internal/instrument"
)

const (
	EntryAfterRelease  = "after_release"
	EntryBeforeRelease = "before_release"

	DirectionLong           = "long"
	DirectionShort          = "short"
	DirectionFollowSurprise = "follow_surprise"
	DirectionFollowInitial  = "follow_initial"
	DirectionFadeInitial    = "fade_initial"

	SurpriseAll      = "all"
	SurprisePositive = "positive"
	SurpriseNegative = "negative"
	SurpriseAbsolute = "absolute"
)

// TradeConfig describes one repeatable decision rule. Money values are cents;
// return and stop/target values are decimal fractions (0.01 is one percent).
type TradeConfig struct {
	EntryTiming      string
	DirectionRule    string
	EntryDelay       core.TF
	HoldFor          core.TF
	MinimumSurpriseZ float64
	SurpriseFilter   string
	StartingEquity   int64
	PositionSize     int64
	CostPerTrade     int64
	EstimateCosts    bool
	CostSpec         *instrument.Spec
	StopLoss         float64
	TakeProfit       float64
}

type Trade struct {
	EventID                        string
	EventTime, EntryTime, ExitTime core.Time
	Side                           string
	MarketReturn, StrategyReturn   float64
	GrossPnL, Costs, NetPnL        int64
	MFE, MAE                       float64
	Surprise, SurpriseZ            float64
	HasSurprise                    bool
	ExitReason                     string
	Ambiguous, Whipsaw             bool
}

type TradeSummary struct {
	Trades, Wins, Losses, Whipsaws, Ambiguous int
	GrossPnL, Costs, NetPnL                   int64
	EndingEquity, MaxDrawdown                 int64
	Expectancy, ProfitFactor                  float64
	AverageMFE, AverageMAE                    float64
}

func simulate(req Request, events []event.Event, surprises map[string]surpriseReading) ([]Trade, TradeSummary, []Exclusion, error) {
	config := *req.Trade
	if req.Series.TF.Coarser(core.TFM5) {
		return nil, TradeSummary{}, nil, fmt.Errorf("eventstudy: trade execution requires m5 bars or finer")
	}
	if err := validateTradeConfig(config); err != nil {
		return nil, TradeSummary{}, nil, err
	}

	var trades []Trade
	var excluded []Exclusion
	for _, item := range events {
		if item.Precision != event.PrecisionExact {
			continue
		}
		reading := surprises[item.ID]
		if !matchesSurprise(config, reading) {
			reason := "release did not match the surprise filter"
			if !reading.OK {
				reason = "actual and forecast values are required by the trade rule"
			}
			excluded = append(excluded, Exclusion{EventID: item.ID, Reason: reason})
			continue
		}
		trade, reason := simulateOne(req.Series, item, reading, config)
		if reason != "" {
			excluded = append(excluded, Exclusion{EventID: item.ID, Reason: reason})
			continue
		}
		trades = append(trades, trade)
	}
	return trades, summarizeTrades(trades, config.StartingEquity), excluded, nil
}

func validateTradeConfig(config TradeConfig) error {
	if config.EntryTiming != EntryAfterRelease && config.EntryTiming != EntryBeforeRelease {
		return fmt.Errorf("eventstudy: unsupported entry timing %q", config.EntryTiming)
	}
	switch config.DirectionRule {
	case DirectionLong, DirectionShort, DirectionFollowSurprise, DirectionFollowInitial, DirectionFadeInitial:
	default:
		return fmt.Errorf("eventstudy: unsupported direction rule %q", config.DirectionRule)
	}
	if config.EntryTiming == EntryBeforeRelease && (config.DirectionRule == DirectionFollowInitial || config.DirectionRule == DirectionFadeInitial || config.DirectionRule == DirectionFollowSurprise) {
		return fmt.Errorf("eventstudy: before-release tests require a fixed long or short direction")
	}
	if config.HoldFor.Millis() <= 0 {
		return fmt.Errorf("eventstudy: hold duration must be positive")
	}
	if config.PositionSize <= 0 {
		return fmt.Errorf("eventstudy: position size must be positive")
	}
	if config.StartingEquity < 0 || config.CostPerTrade < 0 || config.StopLoss < 0 || config.TakeProfit < 0 {
		return fmt.Errorf("eventstudy: money, cost, stop, and target values cannot be negative")
	}
	if config.EstimateCosts && config.CostSpec == nil {
		return fmt.Errorf("eventstudy: automatic costs require an instrument specification")
	}
	return nil
}

func matchesSurprise(config TradeConfig, reading surpriseReading) bool {
	filter := config.SurpriseFilter
	if filter == "" || filter == SurpriseAll {
		return config.DirectionRule != DirectionFollowSurprise || reading.OK
	}
	if !reading.OK {
		return false
	}
	switch filter {
	case SurprisePositive:
		return reading.Z >= config.MinimumSurpriseZ
	case SurpriseNegative:
		return reading.Z <= -config.MinimumSurpriseZ
	case SurpriseAbsolute:
		return math.Abs(reading.Z) >= config.MinimumSurpriseZ
	default:
		return false
	}
}

func simulateOne(series *core.Series, item event.Event, reading surpriseReading, config TradeConfig) (Trade, string) {
	entryTarget := item.KnowledgeAt + core.Time(max64(config.EntryDelay.Millis(), series.TF.Millis()))
	if config.EntryTiming == EntryBeforeRelease {
		entryTarget = item.OccurredAt - core.Time(config.EntryDelay.Millis())
	}
	entry := sort.Search(len(series.Time), func(i int) bool { return series.Time[i] >= entryTarget })
	if entry == len(series.Time) || (config.EntryTiming == EntryBeforeRelease && series.Time[entry] >= item.OccurredAt) {
		return Trade{}, "no tradable entry bar for the rule"
	}
	if series.Open[entry] == 0 {
		return Trade{}, "entry bar has no valid open price"
	}

	side, ok := tradeSide(series, item, reading, config, entry)
	if !ok {
		return Trade{}, "the selected direction could not be determined for this market"
	}
	exitTarget := series.Time[entry] + core.Time(config.HoldFor.Millis())
	timedExit := sort.Search(len(series.Time), func(i int) bool { return series.Time[i] >= exitTarget })
	if timedExit == len(series.Time) {
		return Trade{}, "insufficient bars for the selected exit time"
	}

	entryPrice := float64(series.Open[entry])
	exitPrice := float64(series.Open[timedExit])
	exitIndex := timedExit
	exitReason := "time"
	ambiguous := false
	mfe, mae := 0.0, 0.0
	for i := entry; i < timedExit; i++ {
		favorable, adverse := barExcursions(series, i, entryPrice, side)
		mfe = math.Max(mfe, favorable)
		mae = math.Min(mae, adverse)
		hitStop := config.StopLoss > 0 && adverse <= -config.StopLoss
		hitTarget := config.TakeProfit > 0 && favorable >= config.TakeProfit
		if !hitStop && !hitTarget {
			continue
		}
		exitIndex = i
		ambiguous = hitStop && hitTarget
		if hitStop { // Pessimistic ordering when an OHLC bar touches both.
			exitReason = "stop"
			exitPrice = entryPrice * (1 - float64(side)*config.StopLoss)
		} else {
			exitReason = "target"
			exitPrice = entryPrice * (1 + float64(side)*config.TakeProfit)
		}
		break
	}

	marketReturn := (exitPrice - entryPrice) / entryPrice
	strategyReturn := float64(side) * marketReturn
	gross := int64(math.Round(float64(config.PositionSize) * strategyReturn))
	costs := tradeCosts(config, series.Open[entry])
	return Trade{
		EventID: item.ID, EventTime: item.OccurredAt, EntryTime: series.Time[entry], ExitTime: series.Time[exitIndex],
		Side: sideName(side), MarketReturn: marketReturn, StrategyReturn: strategyReturn,
		GrossPnL: gross, Costs: costs, NetPnL: gross - costs,
		MFE: mfe, MAE: mae, Surprise: reading.Value, SurpriseZ: reading.Z, HasSurprise: reading.OK,
		ExitReason: exitReason, Ambiguous: ambiguous, Whipsaw: mfe > 0.001 && mae < -0.001,
	}, ""
}

func tradeCosts(config TradeConfig, entry core.Price) int64 {
	if !config.EstimateCosts || config.CostSpec == nil || entry <= 0 {
		return config.CostPerTrade
	}
	costModel := config.CostSpec.Costs
	// One full spread is crossed over the round trip. Fixed slippage is paid
	// independently at entry and exit.
	ticks := costModel.SpreadTicks
	if costModel.SlippageMode == instrument.SlippageFixed {
		ticks += 2 * costModel.SlippageTicks
	}
	cost := int64(math.Round(float64(config.PositionSize) * float64(ticks) / float64(entry)))
	switch costModel.CommissionMode {
	case instrument.CommissionPerTrade:
		converted, err := config.CostSpec.QuoteMinorCents(costModel.CommissionFlat, entry)
		if err == nil {
			cost += int64(converted)
		}
	case instrument.CommissionBps:
		cost += int64(math.Round(float64(config.PositionSize) * costModel.CommissionBps.Float64()))
	}
	return max64(cost, 0)
}

func tradeSide(series *core.Series, item event.Event, reading surpriseReading, config TradeConfig, entry int) (int, bool) {
	switch config.DirectionRule {
	case DirectionLong:
		return 1, true
	case DirectionShort:
		return -1, true
	case DirectionFollowSurprise:
		if !reading.OK || reading.Value == 0 {
			return 0, false
		}
		polarity := usdPolarity(series.Symbol)
		if polarity == 0 {
			return 0, false
		}
		return sign(reading.Value) * polarity, true
	case DirectionFollowInitial, DirectionFadeInitial:
		release := sort.Search(len(series.Time), func(i int) bool { return series.Time[i] >= item.KnowledgeAt })
		if release == len(series.Time) || series.Open[release] == series.Open[entry] {
			return 0, false
		}
		direction := 1
		if series.Open[entry] < series.Open[release] {
			direction = -1
		}
		if config.DirectionRule == DirectionFadeInitial {
			direction *= -1
		}
		return direction, true
	default:
		return 0, false
	}
}

func usdPolarity(symbol string) int {
	if colon := strings.LastIndex(symbol, ":"); colon >= 0 {
		symbol = symbol[colon+1:]
	}
	symbol = strings.ToUpper(strings.ReplaceAll(symbol, "/", ""))
	if len(symbol) < 6 {
		return 0
	}
	base, quote := symbol[:3], symbol[3:6]
	if base == "USD" {
		return 1
	}
	if quote == "USD" {
		return -1
	}
	return 0
}

func barExcursions(series *core.Series, i int, entryPrice float64, side int) (float64, float64) {
	if side > 0 {
		return (float64(series.High[i]) - entryPrice) / entryPrice, (float64(series.Low[i]) - entryPrice) / entryPrice
	}
	return (entryPrice - float64(series.Low[i])) / entryPrice, (entryPrice - float64(series.High[i])) / entryPrice
}

func summarizeTrades(trades []Trade, startingEquity int64) TradeSummary {
	summary := TradeSummary{Trades: len(trades), EndingEquity: startingEquity}
	equity, peak := startingEquity, startingEquity
	var grossWins, grossLosses int64
	for _, trade := range trades {
		summary.GrossPnL += trade.GrossPnL
		summary.Costs += trade.Costs
		summary.NetPnL += trade.NetPnL
		summary.AverageMFE += trade.MFE
		summary.AverageMAE += trade.MAE
		if trade.NetPnL > 0 {
			summary.Wins++
			grossWins += trade.NetPnL
		} else if trade.NetPnL < 0 {
			summary.Losses++
			grossLosses += -trade.NetPnL
		}
		if trade.Whipsaw {
			summary.Whipsaws++
		}
		if trade.Ambiguous {
			summary.Ambiguous++
		}
		equity += trade.NetPnL
		peak = max64(peak, equity)
		summary.MaxDrawdown = max64(summary.MaxDrawdown, peak-equity)
	}
	if len(trades) > 0 {
		summary.Expectancy = float64(summary.NetPnL) / float64(len(trades))
		summary.AverageMFE /= float64(len(trades))
		summary.AverageMAE /= float64(len(trades))
	}
	if grossLosses > 0 {
		summary.ProfitFactor = float64(grossWins) / float64(grossLosses)
	}
	summary.EndingEquity = equity
	return summary
}

func sign(value float64) int {
	if value < 0 {
		return -1
	}
	return 1
}

func sideName(side int) string {
	if side < 0 {
		return DirectionShort
	}
	return DirectionLong
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
