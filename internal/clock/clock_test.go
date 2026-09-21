package clock

import (
	"testing"
	"time"
)

func TestParseRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantLabel string
		wantMult  int
	}{
		{name: "default empty", input: "", wantLabel: Rate4h, wantMult: 14400},
		{name: "missing whitespace", input: "  ", wantLabel: Rate4h, wantMult: 14400},
		{name: "one hour", input: Rate1h, wantLabel: Rate1h, wantMult: 3600},
		{name: "four hours", input: Rate4h, wantLabel: Rate4h, wantMult: 14400},
		{name: "twelve hours", input: Rate12h, wantLabel: Rate12h, wantMult: 43200},
		{name: "invalid falls back", input: "fast", wantLabel: Rate4h, wantMult: 14400},
		{name: "invalid unit", input: "4h", wantLabel: Rate4h, wantMult: 14400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseRate(tt.input)
			if got.Label != tt.wantLabel || got.Multiplier != tt.wantMult {
				t.Fatalf("ParseRate(%q) = %+v, want label=%s multiplier=%d",
					tt.input, got, tt.wantLabel, tt.wantMult)
			}
		})
	}
}

func TestFromEnv_DefaultAndValid(t *testing.T) {
	t.Run("missing env is 4h/s", func(t *testing.T) {
		t.Setenv(EnvRate, "")
		clk := FromEnv()
		if clk.RateLabel() != Rate4h || clk.Multiplier() != 14400 {
			t.Fatalf("rate = %s ×%d, want 4h/s ×14400", clk.RateLabel(), clk.Multiplier())
		}
	})

	t.Run("12h/s", func(t *testing.T) {
		t.Setenv(EnvRate, Rate12h)
		clk := FromEnv()
		if clk.RateLabel() != Rate12h || clk.Multiplier() != 43200 {
			t.Fatalf("rate = %s ×%d, want 12h/s ×43200", clk.RateLabel(), clk.Multiplier())
		}
	})

	t.Run("1h/s", func(t *testing.T) {
		t.Setenv(EnvRate, Rate1h)
		clk := FromEnv()
		if clk.RateLabel() != Rate1h || clk.Multiplier() != 3600 {
			t.Fatalf("rate = %s ×%d, want 1h/s ×3600", clk.RateLabel(), clk.Multiplier())
		}
	})

	t.Run("invalid does not panic", func(t *testing.T) {
		t.Setenv(EnvRate, "nope")
		clk := FromEnv()
		if clk.RateLabel() != Rate4h {
			t.Fatalf("rate = %s, want 4h/s", clk.RateLabel())
		}
	})
}

func TestClock_AdvancesFourHoursPerRealSecond(t *testing.T) {
	t.Parallel()

	originSim := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	originReal := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clk := Fixed(ParseRate(Rate4h), originSim, originReal)

	if got := clk.Simulated(); !got.Equal(originSim) {
		t.Fatalf("simulated at t0 = %s, want %s", got, originSim)
	}
	if got := clk.Real(); !got.Equal(originReal) {
		t.Fatalf("real at t0 = %s, want %s", got, originReal)
	}

	clk.SetReal(originReal.Add(time.Second))
	want := originSim.Add(4 * time.Hour)
	if got := clk.Simulated(); !got.Equal(want) {
		t.Fatalf("simulated after 1s = %s, want %s", got, want)
	}
	if clk.Real().Equal(clk.Simulated()) {
		t.Fatal("real time must stay independent of the simulated calendar")
	}
}

func TestClock_Snapshot(t *testing.T) {
	t.Parallel()

	originSim := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	originReal := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clk := Fixed(ParseRate(Rate12h), originSim, originReal)
	snap := clk.Snapshot()
	if snap.Rate != Rate12h || snap.Multiplier != 43200 {
		t.Fatalf("snapshot rate = %s ×%d, want 12h/s ×43200", snap.Rate, snap.Multiplier)
	}
	if !snap.Simulated.Equal(originSim) || !snap.Real.Equal(originReal) {
		t.Fatalf("snapshot times = sim %s real %s", snap.Simulated, snap.Real)
	}
}
