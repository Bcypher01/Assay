package semantic

import "github.com/bcypher01/assay/internal/core"

// PrecedentCandle is one price-independent candle in a 0..1000 window range.
// It is public so setup-test results can report the same score used by the
// executable detector instead of approximating it in the UI.
type PrecedentCandle struct {
	Open, High, Low, Close int
}

// PrecedentScore explains why a candidate did or did not qualify. All scores
// use 0..1000 so they map directly to the user's similarity percentage.
type PrecedentScore struct {
	Overall   int
	Shape     int
	Structure int
	Direction int
	Anatomy   int
	Trigger   int
	Accepted  bool
}

// ScoreSeriesPrecedent normalizes the n candles ending at signalBar and scores
// them with the same structural matcher used during primitive evaluation.
func ScoreSeriesPrecedent(expected []PrecedentCandle, s *core.Series, signalBar, threshold int) PrecedentScore {
	n := len(expected)
	from := signalBar - n + 1
	if s == nil || n < 3 || from < 0 || signalBar >= s.Len() {
		return PrecedentScore{}
	}
	low, high := s.Low[from], s.High[from]
	for i := from + 1; i <= signalBar; i++ {
		low = core.Min(low, s.Low[i])
		high = core.Max(high, s.High[i])
	}
	span := high - low
	if span <= 0 {
		return PrecedentScore{}
	}
	norm := func(price core.Price) int {
		return int(((price-low)*1000 + span/2) / span)
	}
	candidate := make([]PrecedentCandle, n)
	for i := range candidate {
		at := from + i
		candidate[i] = PrecedentCandle{
			Open: norm(s.Open[at]), High: norm(s.High[at]),
			Low: norm(s.Low[at]), Close: norm(s.Close[at]),
		}
	}
	return ScorePrecedent(expected, candidate, threshold)
}

// ScorePrecedent compares the path, swing order, candle direction/anatomy and
// final trigger segment separately. A weighted average is never sufficient:
// critical component floors and outlier gates must also pass.
func ScorePrecedent(expected, candidate []PrecedentCandle, threshold int) PrecedentScore {
	if len(expected) < 3 || len(expected) != len(candidate) {
		return PrecedentScore{}
	}
	n := len(expected)
	clampScore := func(error int) int {
		if error >= 1000 {
			return 0
		}
		if error <= 0 {
			return 1000
		}
		return 1000 - error
	}

	shapeError, anatomyError, maxCandleError := 0, 0, 0
	directionComparable, directionMatches := 0, 0
	for i := range expected {
		e, c := expected[i], candidate[i]
		// Close position carries the path; the body midpoint stops two candles
		// with equal closes but very different opens from appearing identical.
		shapeError += absInt(e.Close-c.Close) + absInt((e.Open+e.Close)-(c.Open+c.Close))/2

		eRange, cRange := maxInt(1, e.High-e.Low), maxInt(1, c.High-c.Low)
		eBody, cBody := absInt(e.Close-e.Open)*1000/eRange, absInt(c.Close-c.Open)*1000/cRange
		eUpper := (e.High - maxInt(e.Open, e.Close)) * 1000 / eRange
		cUpper := (c.High - maxInt(c.Open, c.Close)) * 1000 / cRange
		eLower := (minInt(e.Open, e.Close) - e.Low) * 1000 / eRange
		cLower := (minInt(c.Open, c.Close) - c.Low) * 1000 / cRange
		candleError := (absInt(eBody-cBody) + absInt(eUpper-cUpper) + absInt(eLower-cLower)) / 3
		anatomyError += candleError
		maxCandleError = maxInt(maxCandleError, candleError)

		ePolarity, cPolarity := sign(e.Close-e.Open), sign(c.Close-c.Open)
		// Dojis do not impose an arbitrary bullish/bearish requirement.
		if ePolarity != 0 {
			directionComparable++
			if ePolarity == cPolarity {
				directionMatches++
			}
		}
	}

	shape := clampScore(shapeError / (n * 2))
	anatomy := clampScore(anatomyError / n)
	direction := 1000
	if directionComparable > 0 {
		direction = directionMatches * 1000 / directionComparable
	}

	// Structure compares consecutive close movement and, independently, the
	// order of meaningful turning points. This makes reversed or shuffled paths
	// fail even when their global high/low boxes look alike.
	moveError, moveComparable, turnComparable, turnMatches := 0, 0, 0, 0
	for i := 1; i < n; i++ {
		eMove := expected[i].Close - expected[i-1].Close
		cMove := candidate[i].Close - candidate[i-1].Close
		moveError += absInt(eMove-cMove) / 2
		moveComparable++
		if absInt(eMove) >= 25 {
			turnComparable++
			if sign(eMove) == sign(cMove) {
				turnMatches++
			}
		}
	}
	moveScore := 1000
	if moveComparable > 0 {
		moveScore = clampScore(moveError / moveComparable)
	}
	turnScore := 1000
	if turnComparable > 0 {
		turnScore = turnMatches * 1000 / turnComparable
	}
	structure := (moveScore*3 + turnScore*2) / 5

	triggerFrom := maxInt(0, n-3)
	triggerError := 0
	for i := triggerFrom; i < n; i++ {
		e, c := expected[i], candidate[i]
		triggerError += absInt(e.Open-c.Open) + absInt(e.Close-c.Close)
	}
	trigger := clampScore(triggerError / ((n - triggerFrom) * 2))

	overall := (shape*35 + structure*25 + direction*15 + anatomy*15 + trigger*10) / 100
	floor := maxInt(600, threshold-120)

	// Hard gates prevent the weighted score from hiding a structurally fatal
	// mismatch. Small net moves are treated as ranges and do not impose a side.
	netExpected := expected[n-1].Close - expected[0].Open
	netCandidate := candidate[n-1].Close - candidate[0].Open
	netDirectionOK := absInt(netExpected) < 80 || (sign(netExpected) == sign(netCandidate) && absInt(netCandidate) >= absInt(netExpected)/3)
	directionFloor := maxInt(700, threshold-50)
	directionOK := directionComparable < 3 || direction >= directionFloor
	turnsOK := turnComparable < 3 || turnScore >= 650
	outlierLimit := maxInt(260, (1000-threshold)*2+100)
	outliersOK := maxCandleError <= outlierLimit

	return PrecedentScore{
		Overall: overall, Shape: shape, Structure: structure,
		Direction: direction, Anatomy: anatomy, Trigger: trigger,
		Accepted: overall >= threshold && shape >= floor && structure >= floor &&
			trigger >= floor && directionOK && turnsOK && outliersOK && netDirectionOK,
	}
}

func sign(value int) int {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
