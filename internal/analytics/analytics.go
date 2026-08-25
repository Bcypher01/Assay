package analytics

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"sync"

	"github.com/Bcypher01/Assay/internal/ir"
)

// Bounded is the mandatory representation for an ambiguity-affected metric.
type Bounded struct{ Pessimistic, Optimistic float64 }
type Metrics struct {
	FinalEquity, MaxDrawdown, AverageDrawdown, ProfitFactor, Expectancy Bounded
	Trades, Wins, Losses, LongestLosingStreak                           int
}
type StudyResult struct {
	OutOfSample, InSample Metrics
	Distribution          MCDistribution
	Sensitivity           Grid
	Trials                int
	Deflated              Metrics
	CostCurve             []Metrics
}

// Study is the reproducible input to an analytics run.  Snapshot identifies
// the immutable market-data snapshot used for the run; Scope identifies the
// instrument set and period used for multiple-testing disclosure.
type Study struct {
	Strategy    *ir.Strategy
	Sweep       []ParamAxis
	WalkForward WFConfig
	MonteCarlo  MCConfig
	Seed        int64
	Snapshot    string
	Scope       Scope
}

// Scope deliberately lives with the study document so persistence does not
// need to infer a period from a result after the fact.
type Scope struct {
	Instruments []string
	Period      string
}

// Returns contains each trade's pessimistic and optimistic PnL in account units.
type Returns struct{ Pessimistic, Optimistic []float64 }

func (r Returns) Validate() error {
	if len(r.Pessimistic) != len(r.Optimistic) {
		return fmt.Errorf("analytics: mismatched return bounds")
	}
	return nil
}
func MetricsFor(r Returns) (Metrics, error) {
	if err := r.Validate(); err != nil {
		return Metrics{}, err
	}
	return metrics(r.Pessimistic, r.Optimistic), nil
}
func metrics(p, o []float64) Metrics {
	result := Metrics{Trades: len(p)}
	result.FinalEquity = Bounded{sum(p), sum(o)}
	result.MaxDrawdown = Bounded{maxDD(p), maxDD(o)}
	result.AverageDrawdown = Bounded{avgDD(p), avgDD(o)}
	result.ProfitFactor = Bounded{profitFactor(p), profitFactor(o)}
	result.Expectancy = Bounded{mean(p), mean(o)}
	for _, x := range p {
		if x > 0 {
			result.Wins++
		} else if x < 0 {
			result.Losses++
		}
	}
	result.LongestLosingStreak = losingStreak(p)
	return result
}
func sum(x []float64) float64 {
	var s float64
	for _, n := range x {
		s += n
	}
	return s
}
func mean(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	return sum(x) / float64(len(x))
}
func profitFactor(x []float64) float64 {
	var g, l float64
	for _, n := range x {
		if n > 0 {
			g += n
		} else {
			l -= n
		}
	}
	if l == 0 {
		return g
	}
	return g / l
}
func maxDD(x []float64) float64 {
	peak, equity, worst := 0.0, 0.0, 0.0
	for _, n := range x {
		equity += n
		if equity > peak {
			peak = equity
		}
		if peak-equity > worst {
			worst = peak - equity
		}
	}
	return worst
}
func avgDD(x []float64) float64 {
	peak, equity, total, n := 0.0, 0.0, 0.0, 0
	for _, v := range x {
		equity += v
		if equity > peak {
			peak = equity
		}
		if peak > equity {
			total += peak - equity
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return total / float64(n)
}
func losingStreak(x []float64) int {
	best, run := 0, 0
	for _, v := range x {
		if v < 0 {
			run++
			if run > best {
				best = run
			}
		} else {
			run = 0
		}
	}
	return best
}

type ParamAxis struct {
	Name   string
	Values []string
}
type SweepRun func(context.Context, []string) (Returns, error)
type SweepResult struct {
	Values  []string
	Metrics Metrics
	Err     error
}

// Sweep uses a bounded worker pool and indexed assignment, so ordering is stable.
func Sweep(ctx context.Context, axes []ParamAxis, run SweepRun) []SweepResult {
	configs := cartesian(axes)
	out := make([]SweepResult, len(configs))
	jobs := make(chan int)
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					out[i] = SweepResult{Values: configs[i], Err: err}
					continue
				}
				r, e := run(ctx, configs[i])
				m, me := MetricsFor(r)
				if e == nil {
					e = me
				}
				out[i] = SweepResult{Values: configs[i], Metrics: m, Err: e}
			}
		}()
	}
	for i := range configs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}
func cartesian(axes []ParamAxis) [][]string {
	out := [][]string{{}}
	for _, a := range axes {
		next := make([][]string, 0, len(out)*len(a.Values))
		for _, p := range out {
			for _, v := range a.Values {
				q := append(append([]string(nil), p...), v)
				next = append(next, q)
			}
		}
		out = next
	}
	return out
}

type WFConfig struct {
	Train, Test, Step int
	Anchored          bool
}
type WFWindow struct{ TrainFrom, TrainTo, TestFrom, TestTo int }

func WalkForwardWindows(n int, c WFConfig) ([]WFWindow, error) {
	if c.Train <= 0 || c.Test <= 0 || c.Step <= 0 {
		return nil, fmt.Errorf("analytics: walk-forward windows must be positive")
	}
	var out []WFWindow
	for start := 0; ; start += c.Step {
		trainFrom := start
		if c.Anchored {
			trainFrom = 0
		}
		trainTo := start + c.Train
		testTo := trainTo + c.Test
		if testTo > n {
			break
		}
		out = append(out, WFWindow{trainFrom, trainTo, trainTo, testTo})
	}
	return out, nil
}

type MCConfig struct {
	Samples, BlockLength int
	Seed                 uint64
}
type MCDistribution struct {
	FinalEquity, MaxDrawdown        []float64
	RealisedFinal, RealisedDrawdown float64
}

func MonteCarlo(r Returns, c MCConfig) (MCDistribution, error) {
	if err := r.Validate(); err != nil {
		return MCDistribution{}, err
	}
	if c.Samples <= 0 {
		return MCDistribution{}, fmt.Errorf("analytics: samples must be positive")
	}
	if c.BlockLength <= 0 {
		c.BlockLength = 1
	}
	n := len(r.Pessimistic)
	d := MCDistribution{FinalEquity: make([]float64, c.Samples), MaxDrawdown: make([]float64, c.Samples), RealisedFinal: sum(r.Pessimistic), RealisedDrawdown: maxDD(r.Pessimistic)}
	if n == 0 {
		return d, nil
	}
	rng := rand.New(rand.NewPCG(c.Seed, c.Seed^0x9e3779b97f4a7c15))
	sample := make([]float64, n)
	for k := range d.FinalEquity {
		for i := 0; i < n; {
			start := rng.IntN(n)
			for j := 0; j < c.BlockLength && i < n; j++ {
				sample[i] = r.Pessimistic[(start+j)%n]
				i++
			}
		}
		d.FinalEquity[k], d.MaxDrawdown[k] = sum(sample), maxDD(sample)
	}
	sort.Float64s(d.FinalEquity)
	sort.Float64s(d.MaxDrawdown)
	return d, nil
}

type Grid struct {
	X, Y  []string
	Cells [][]Metrics
}

func Sensitivity(x, y ParamAxis, run SweepRun, ctx context.Context) Grid {
	g := Grid{X: append([]string(nil), x.Values...), Y: append([]string(nil), y.Values...), Cells: make([][]Metrics, len(y.Values))}
	for i := range g.Cells {
		g.Cells[i] = make([]Metrics, len(x.Values))
		for j := range g.Cells[i] {
			r, _ := run(ctx, []string{x.Values[j], y.Values[i]})
			g.Cells[i][j], _ = MetricsFor(r)
		}
	}
	return g
}

// Deflate applies a conservative sqrt(2 log trials) multiple-testing penalty.
func Deflate(m Metrics, trials int) Metrics {
	if trials <= 1 {
		return m
	}
	p := math.Sqrt(2 * math.Log(float64(trials)))
	m.Expectancy.Pessimistic -= p
	m.Expectancy.Optimistic -= p
	return m
}
