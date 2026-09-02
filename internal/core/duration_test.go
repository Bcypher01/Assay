package core

import (
	"encoding/json"
	"testing"
)

func TestDurationString(t *testing.T) {
	cases := []struct {
		d    Duration
		want string
	}{
		{0, "0"},
		{Week, "1w"},
		{Day, "1d"},
		{4 * Hour, "4h"},
		{45 * Minute, "45m"},
		{90 * Minute, "90m"},
		{30 * Second, "30s"},
		{7 * Millisecond, "7ms"},
	}
	for _, c := range cases {
		if got := c.d.String(); got != c.want {
			t.Fatalf("Duration(%d).String() = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want Duration
	}{
		{"0", 0},
		{"1w", Week},
		{"3d", 3 * Day},
		{"4h", 4 * Hour},
		{"45m", 45 * Minute},
		{"30s", 30 * Second},
		{"7ms", 7 * Millisecond},
	}
	for _, c := range cases {
		got, err := ParseDuration(c.in)
		if err != nil || got != c.want {
			t.Fatalf("ParseDuration(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
	// A bare number carries no unit, and guessing one is wrong by orders of
	// magnitude and silent. Compound forms are refused for the same reason.
	for _, in := range []string{"", "3", "1h30m", "3 hours", "h", "-", "xm"} {
		if got, err := ParseDuration(in); err == nil {
			t.Fatalf("ParseDuration(%q) = %d, want an error", in, got)
		}
	}
}

func TestDurationJSONRoundTrip(t *testing.T) {
	type doc struct {
		Window Duration `json:"window"`
	}
	b, err := json.Marshal(doc{Window: 45 * Minute})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"window":"45m"}` {
		t.Fatalf("marshal = %s", b)
	}
	var back doc
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Window != 45*Minute {
		t.Fatalf("round trip = %d", back.Window)
	}
	if err := json.Unmarshal([]byte(`{"window":45}`), &back); err == nil {
		t.Fatal("a unitless number must not decode as a duration")
	}
}
