package core

import "testing"

func TestTFParseAndString(t *testing.T) {
	for tf := TFTick; tf < tfCount; tf++ {
		got, err := ParseTF(tf.String())
		if err != nil {
			t.Fatalf("ParseTF(%q): %v", tf.String(), err)
		}
		if got != tf {
			t.Fatalf("round trip of %v gave %v", tf, got)
		}
	}
	if _, err := ParseTF("15m"); err == nil {
		t.Fatal("expected an error for a non-canonical name")
	}
}

func TestTFOrdering(t *testing.T) {
	if !TFM5.Finer(TFM15) {
		t.Fatal("m5 should be finer than m15")
	}
	if !TFH4.Coarser(TFH1) {
		t.Fatal("h4 should be coarser than h1")
	}
	if TFM15.Finer(TFM15) || TFM15.Coarser(TFM15) {
		t.Fatal("a timeframe is neither finer nor coarser than itself")
	}
	if !TFTick.Finer(TFM1) || TFTick.Coarser(TFM1) {
		t.Fatal("tick is finer than everything")
	}
}

func TestAlignDown(t *testing.T) {
	cases := []struct {
		name string
		tf   TF
		in   string
		want string
	}{
		{"m15 mid-bar", TFM15, "2026-08-22T10:07:31Z", "2026-08-22T10:00:00Z"},
		{"m15 on boundary", TFM15, "2026-08-22T10:15:00Z", "2026-08-22T10:15:00Z"},
		{"h4", TFH4, "2026-08-22T10:07:31Z", "2026-08-22T08:00:00Z"},
		{"d1", TFD1, "2026-08-22T23:59:59Z", "2026-08-22T00:00:00Z"},
		{"w1 lands on Monday", TFW1, "2026-08-22T10:00:00Z", "2026-08-17T00:00:00Z"},
		{"w1 on a Monday", TFW1, "2026-08-17T00:00:00Z", "2026-08-17T00:00:00Z"},
		{"pre-epoch floors down", TFH1, "1969-12-31T23:30:00Z", "1969-12-31T23:00:00Z"},
		{"year boundary", TFD1, "2027-01-01T00:00:01Z", "2027-01-01T00:00:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.tf.AlignDown(MustParseTime(c.in))
			want := MustParseTime(c.want)
			if got != want {
				t.Fatalf("got %s, want %s", got.Std(), want.Std())
			}
		})
	}
}

func TestW1AlignmentIsAlwaysMonday(t *testing.T) {
	start := MustParseTime("2019-01-01T00:00:00Z")
	for i := 0; i < 500; i++ {
		ts := start + Time(int64(i)*dayMS)
		got := TFW1.AlignDown(ts).Std()
		if got.Weekday().String() != "Monday" {
			t.Fatalf("week alignment of %s landed on %s", ts.Std(), got.Weekday())
		}
		if got.Hour() != 0 || got.Minute() != 0 {
			t.Fatalf("week alignment of %s is not midnight: %s", ts.Std(), got)
		}
	}
}

func TestContainsIsHalfOpen(t *testing.T) {
	open := MustParseTime("2026-08-22T10:00:00Z")
	if !TFM15.Contains(open, open) {
		t.Fatal("the open instant belongs to the bar")
	}
	if !TFM15.Contains(open, open+Time(minuteMS*14)) {
		t.Fatal("an instant inside the bar belongs to it")
	}
	if TFM15.Contains(open, TFM15.CloseOf(open)) {
		t.Fatal("the close instant belongs to the next bar, not this one")
	}
}
