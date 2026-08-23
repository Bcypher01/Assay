// Package instrument defines instrument specifications, session calendars and
// cost models. Everything that differs between a crypto perpetual, a spot FX
// pair and a stock CFD lives here as data, so that no asset-class logic leaks
// into the engine.
package instrument

import (
	"errors"
	"fmt"
	"strings"
)

// ErrBadDecimal is returned for input that is not a plain decimal number.
var ErrBadDecimal = errors.New("instrument: malformed decimal")

// parseDecimal parses a plain decimal string into a mantissa and the number of
// fractional digits, e.g. "1.08532" -> (108532, 5).
//
// Deliberately not strconv.ParseFloat: routing a price through float64 loses
// exactness for values a binary float cannot represent, and exactness is the
// entire reason prices are fixed point.
func parseDecimal(s string) (mant int64, frac int, err error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, 0, fmt.Errorf("%w: empty", ErrBadDecimal)
	}
	neg := false
	switch t[0] {
	case '+':
		t = t[1:]
	case '-':
		neg = true
		t = t[1:]
	}
	if t == "" {
		return 0, 0, fmt.Errorf("%w: %q", ErrBadDecimal, s)
	}

	seenDot := false
	seenDigit := false
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case c == '.':
			if seenDot {
				return 0, 0, fmt.Errorf("%w: %q", ErrBadDecimal, s)
			}
			seenDot = true
		case c >= '0' && c <= '9':
			seenDigit = true
			d := int64(c - '0')
			if mant > (1<<62)/10 {
				return 0, 0, fmt.Errorf("%w: %q overflows", ErrBadDecimal, s)
			}
			mant = mant*10 + d
			if seenDot {
				frac++
			}
		default:
			return 0, 0, fmt.Errorf("%w: %q", ErrBadDecimal, s)
		}
	}
	if !seenDigit {
		return 0, 0, fmt.Errorf("%w: %q", ErrBadDecimal, s)
	}
	if neg {
		mant = -mant
	}
	return mant, frac, nil
}

// formatDecimal renders mant scaled by 10^-frac, trimming nothing: the caller
// controls precision by choosing frac.
func formatDecimal(mant int64, frac int) string {
	neg := mant < 0
	if neg {
		mant = -mant
	}
	digits := fmt.Sprintf("%d", mant)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	if frac == 0 {
		b.WriteString(digits)
		return b.String()
	}
	for len(digits) <= frac {
		digits = "0" + digits
	}
	b.WriteString(digits[:len(digits)-frac])
	b.WriteByte('.')
	b.WriteString(digits[len(digits)-frac:])
	return b.String()
}

// pow10 returns 10^n for n in [0, 18].
func pow10(n int) int64 {
	var v int64 = 1
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}
