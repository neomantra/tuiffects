package tuiffects

import "time"

// Clock is the engine's source of time, in seconds.
//
// A handful of effects are written against the wall clock rather than against
// a frame count: matrix runs its rain for a number of seconds, thunderstorm
// runs its storm for a number of seconds. Everything else in this engine is
// reproducible from a seed, and a real clock would take that away from those
// two: the same seed would give a different number of frames on a fast machine
// than on a slow one, and a test could not pin either.
//
// So the default is a virtual clock. It advances a fixed step once per
// Engine.Update, which every effect calls exactly once per frame, and it
// therefore reports the time the animation would have taken if the host had
// kept up. A seeded run stays reproducible and "run the rain for eight
// seconds" still means eight seconds of animation.
//
// The step is 1/frameRate. Set it to the rate the host actually paints at, or
// a two-second effect will not last two seconds on the screen.
//
// A host that would rather have real elapsed time, and does not mind that the
// two clock-driven effects stop being reproducible, can put a real clock on
// the engine instead. Nothing else in the package reads the clock.
type Clock struct {
	virtual bool

	// now is the virtual clock's position, in seconds from zero.
	now float64
	// step is how far one frame moves the virtual clock.
	step float64

	// start and wallStart are the real clock's two origins: the monotonic one
	// the runtime gives, and the Unix time it was taken at.
	start     time.Time
	wallStart float64
}

// NewVirtualClock builds the default clock: time advances step by step with
// the frames, not with the machine. A frame rate at or below zero means sixty.
func NewVirtualClock(frameRate int) *Clock {
	step := 1.0 / 60.0
	if frameRate > 0 {
		step = 1.0 / float64(frameRate)
	}
	return &Clock{virtual: true, step: step}
}

// NewRealClock builds a clock that reads the machine. An effect driven by it
// is no longer reproducible from its seed, so use it only when the host wants
// wall time more than it wants a repeatable run.
func NewRealClock() *Clock {
	now := time.Now()
	return &Clock{start: now, wallStart: float64(now.UnixNano()) / 1e9}
}

// Wall is seconds since the Unix epoch, upstream's time.time(). The virtual
// clock has no epoch, so it reports seconds since the run started, the same
// number Elapsed gives. Effects only ever subtract two readings of it, so the
// origin does not matter to them.
func (c *Clock) Wall() float64 {
	if c == nil {
		return 0
	}
	if c.virtual {
		return c.now
	}
	return c.wallStart + time.Since(c.start).Seconds()
}

// Elapsed is seconds since the run started, upstream's time.monotonic().
func (c *Clock) Elapsed() float64 {
	if c == nil {
		return 0
	}
	if c.virtual {
		return c.now
	}
	return time.Since(c.start).Seconds()
}

// AdvanceFrame moves a virtual clock on by one frame and does nothing to a
// real one. Engine.Update calls it, so an effect never has to; calling it as
// well would run the clock at twice the frame rate.
func (c *Clock) AdvanceFrame() {
	if c == nil || !c.virtual {
		return
	}
	c.now += c.step
}
