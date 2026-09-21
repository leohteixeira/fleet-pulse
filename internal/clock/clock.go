// Package clock is the environment-only simulated calendar.
package clock

import (
	"math"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// EnvRate is the environment variable that selects the calendar rate.
	EnvRate = "CALENDAR_RATE"
	// Rate1h advances one simulated hour per real second.
	Rate1h = "1h/s"
	// Rate4h is the default calendar rate (×14400).
	Rate4h = "4h/s"
	// Rate12h advances twelve simulated hours per real second.
	Rate12h = "12h/s"

	mult1h  = 3600
	mult4h  = 14400
	mult12h = 43200
)

// Origin is the simulated instant when a process clock starts.
var Origin = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Rate is a calendar advance setting. Invalid or missing values become Rate4h.
type Rate struct {
	Label      string
	Multiplier int
}

// Snapshot is the calendar view served by GET /api/clock. It is not an HTTP type.
type Snapshot struct {
	Simulated  time.Time
	Real       time.Time
	Rate       string
	Multiplier int
}

// Clock advances a simulated calendar from elapsed real time × rate.
type Clock struct {
	mu         sync.Mutex
	originSim  time.Time
	originReal time.Time
	rate       Rate
	frozen     *time.Time
}

// ParseRate maps 1h/s, 4h/s, and 12h/s. Anything else, including empty, is 4h/s.
func ParseRate(raw string) Rate {
	switch strings.TrimSpace(raw) {
	case Rate1h:
		return Rate{Label: Rate1h, Multiplier: mult1h}
	case Rate12h:
		return Rate{Label: Rate12h, Multiplier: mult12h}
	default:
		return Rate{Label: Rate4h, Multiplier: mult4h}
	}
}

// FromEnv builds a live clock from CALENDAR_RATE. Invalid or missing → 4h/s.
func FromEnv() *Clock {
	return New(ParseRate(os.Getenv(EnvRate)))
}

// New starts a live clock at Origin, using time.Now as the real origin.
func New(rate Rate) *Clock {
	if rate.Multiplier <= 0 {
		rate = ParseRate("")
	}
	return &Clock{
		originSim:  Origin,
		originReal: time.Now(),
		rate:       rate,
	}
}

// Fixed is a deterministic clock for tests. Real time is frozen at originReal.
func Fixed(rate Rate, originSim, originReal time.Time) *Clock {
	if rate.Multiplier <= 0 {
		rate = ParseRate("")
	}
	t := originReal
	return &Clock{
		originSim:  originSim,
		originReal: originReal,
		rate:       rate,
		frozen:     &t,
	}
}

// SetReal moves a frozen clock's real now. Live clocks ignore it.
func (c *Clock) SetReal(now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.frozen == nil {
		return
	}
	*c.frozen = now
}

// Simulated is the calendar instant (origin + elapsed real × multiplier).
func (c *Clock) Simulated() time.Time {
	if c == nil {
		return Origin
	}
	realNow, originReal, originSim, mult := c.snapshot()
	return simulatedAt(realNow, originReal, originSim, mult)
}

// Real is wall-clock time. Telemetry must keep using this, not Simulated.
func (c *Clock) Real() time.Time {
	if c == nil {
		return time.Now()
	}
	realNow, _, _, _ := c.snapshot()
	return realNow
}

// RateLabel is the environment token (4h/s, 1h/s, 12h/s).
func (c *Clock) RateLabel() string {
	if c == nil {
		return Rate4h
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rate.Label == "" {
		return Rate4h
	}
	return c.rate.Label
}

// Rate is the calendar advance setting (label and multiplier).
func (c *Clock) Rate() Rate {
	if c == nil {
		return ParseRate("")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rate.Multiplier <= 0 {
		return ParseRate("")
	}
	return c.rate
}

// Multiplier is the ×N badge (14400 for 4h/s).
func (c *Clock) Multiplier() int {
	if c == nil {
		return mult4h
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rate.Multiplier <= 0 {
		return mult4h
	}
	return c.rate.Multiplier
}

// Snapshot copies simulated time, real time, rate, and multiplier under one lock.
func (c *Clock) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{
			Simulated:  Origin,
			Real:       time.Now(),
			Rate:       Rate4h,
			Multiplier: mult4h,
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	realNow, originReal, originSim, rate := c.stateLocked()
	mult := rate.Multiplier
	if mult <= 0 {
		mult = mult4h
	}
	label := rate.Label
	if label == "" {
		label = Rate4h
	}
	return Snapshot{
		Simulated:  simulatedAt(realNow, originReal, originSim, mult),
		Real:       realNow,
		Rate:       label,
		Multiplier: mult,
	}
}

func (c *Clock) snapshot() (realNow, originReal, originSim time.Time, mult int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	realNow, originReal, originSim, rate := c.stateLocked()
	mult = rate.Multiplier
	if mult <= 0 {
		mult = mult4h
	}
	return realNow, originReal, originSim, mult
}

func (c *Clock) stateLocked() (realNow, originReal, originSim time.Time, rate Rate) {
	originReal = c.originReal
	originSim = c.originSim
	rate = c.rate
	if c.frozen != nil {
		realNow = *c.frozen
		return realNow, originReal, originSim, rate
	}
	return time.Now(), originReal, originSim, rate
}

func simulatedAt(realNow, originReal, originSim time.Time, mult int) time.Time {
	if mult <= 0 {
		mult = mult4h
	}
	elapsed := realNow.Sub(originReal)
	if elapsed < 0 {
		elapsed = 0
	}
	maxElapsed := time.Duration(math.MaxInt64 / int64(mult))
	if elapsed > maxElapsed {
		elapsed = maxElapsed
	}
	return originSim.Add(elapsed * time.Duration(mult))
}
