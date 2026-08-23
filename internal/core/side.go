package core

// Side is the direction of a position or order.
type Side uint8

const (
	SideLong Side = iota + 1
	SideShort
)

// Valid reports whether s is a known trading direction.
func (s Side) Valid() bool { return s == SideLong || s == SideShort }

// Sign returns +1 for long and -1 for short. Invalid sides return zero.
func (s Side) Sign() Price {
	if s == SideLong {
		return 1
	}
	if s == SideShort {
		return -1
	}
	return 0
}
