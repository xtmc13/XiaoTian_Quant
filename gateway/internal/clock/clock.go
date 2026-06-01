package clock

import "time"

// Clock provides abstracted time access for both live and replay modes.
type Clock interface {
	Now() time.Time
	NowMs() int64
	NowNs() int64
}

// RealClock uses the system clock.
type RealClock struct{}

func (c *RealClock) Now() time.Time { return time.Now() }
func (c *RealClock) NowMs() int64   { return time.Now().UnixMilli() }
func (c *RealClock) NowNs() int64   { return time.Now().UnixNano() }

func NewRealClock() *RealClock {
	return &RealClock{}
}

// ReplayClock is used during backtesting to simulate time.
type ReplayClock struct {
	t     time.Time
	ratio float64 // speed multiplier
}

func NewReplayClock(startTime time.Time, speedRatio float64) *ReplayClock {
	if speedRatio <= 0 {
		speedRatio = 1.0
	}
	return &ReplayClock{t: startTime, ratio: speedRatio}
}

func (c *ReplayClock) Now() time.Time { return c.t }
func (c *ReplayClock) NowMs() int64   { return c.t.UnixMilli() }
func (c *ReplayClock) NowNs() int64   { return c.t.UnixNano() }

// Advance moves the clock forward by the given duration (real time, not speed-adjusted).
func (c *ReplayClock) Advance(d time.Duration) {
	c.t = c.t.Add(time.Duration(float64(d) * c.ratio))
}

// AdvanceTo sets the clock to a specific time.
func (c *ReplayClock) AdvanceTo(t time.Time) {
	c.t = t
}

// SetSpeed adjusts the replay speed ratio.
func (c *ReplayClock) SetSpeed(ratio float64) {
	if ratio > 0 {
		c.ratio = ratio
	}
}

// Ensure interfaces are satisfied
var _ Clock = (*RealClock)(nil)
var _ Clock = (*ReplayClock)(nil)
