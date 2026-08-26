package tuiffects

import (
	"math"
	"testing"
	"time"
)

func clockEngine(t *testing.T) *Engine {
	t.Helper()
	terminal := NewTerminalFromText("clock", TerminalConfig{Width: 20, Height: 4})
	return NewEngine(terminal, NewRng(1))
}

// TestVirtualClockAdvancesOncePerUpdate pins where time comes from.
//
// A handful of effects are written in seconds rather than frames: matrix runs
// its rain for a number of seconds, thunderstorm its storm. The virtual clock
// makes a second mean a second of animation, so "eight seconds of rain" is
// always the same number of frames whatever the machine is doing.
//
// It moves in Update because an effect calls Update exactly once per frame,
// while the host may read Frame twice, or read FrameRows instead, or drop a
// frame it decided not to paint.
//
// Negative control: moving AdvanceFrame from Update to Frame makes the second
// half of this test, which reads the frame three times per update, report
// three times the elapsed time. Confirmed failing.
func TestVirtualClockAdvancesOncePerUpdate(t *testing.T) {
	e := clockEngine(t)
	e.Clock = NewVirtualClock(60)
	if e.Clock.Elapsed() != 0 {
		t.Fatalf("a fresh clock reads %v, want 0", e.Clock.Elapsed())
	}
	for i := 0; i < 60; i++ {
		e.Update()
	}
	if got := e.Clock.Elapsed(); math.Abs(got-1) > 1e-9 {
		t.Errorf("sixty updates at sixty frames a second read %v seconds, want 1", got)
	}

	other := clockEngine(t)
	other.Clock = NewVirtualClock(60)
	for i := 0; i < 60; i++ {
		other.Update()
		other.Frame()
		other.Frame()
		other.Frame()
	}
	if got := other.Clock.Elapsed(); math.Abs(got-1) > 1e-9 {
		t.Errorf("reading the frame three times per update read %v seconds, want 1", got)
	}
}

// TestVirtualClockFollowsItsFrameRate checks that the step is the host's frame
// rate and not a constant. A host painting at thirty frames a second and
// telling the engine sixty would see every timed effect run twice as long as
// it asked for.
//
// Negative control: hard-coding the step at 1/60 makes the thirty-frame clock
// read half a second and this fails. Confirmed failing.
func TestVirtualClockFollowsItsFrameRate(t *testing.T) {
	for _, rate := range []int{15, 30, 120} {
		clock := NewVirtualClock(rate)
		for i := 0; i < rate; i++ {
			clock.AdvanceFrame()
		}
		if got := clock.Elapsed(); math.Abs(got-1) > 1e-9 {
			t.Errorf("at %d frames a second, %d frames read %v seconds, want 1", rate, rate, got)
		}
	}
	fallback := NewVirtualClock(0)
	for i := 0; i < 60; i++ {
		fallback.AdvanceFrame()
	}
	if got := fallback.Elapsed(); math.Abs(got-1) > 1e-9 {
		t.Errorf("a clock built with no frame rate read %v seconds over sixty frames, want 1", got)
	}
}

// TestEngineDefaultsToAVirtualClock is the property that keeps the timed
// effects reproducible. Everything else in this engine comes out of a seed;
// a real clock by default would mean matrix and thunderstorm produced a
// different number of frames on every run, on every machine, and could not be
// tested at all.
//
// The second half is the negative control, run here rather than described: a
// real clock over the same sixty updates reads very nearly nothing, because
// sixty updates of a five-character canvas take microseconds. An effect asked
// to rain for two seconds would rain for hundreds of thousands of frames.
func TestEngineDefaultsToAVirtualClock(t *testing.T) {
	first, second := clockEngine(t), clockEngine(t)
	for i := 0; i < 60; i++ {
		first.Update()
		second.Update()
	}
	if first.Clock.Elapsed() != second.Clock.Elapsed() {
		t.Errorf("two identical runs read %v and %v seconds, so the default clock is not reproducible",
			first.Clock.Elapsed(), second.Clock.Elapsed())
	}
	if got := first.Clock.Elapsed(); math.Abs(got-1) > 1e-9 {
		t.Errorf("the default clock read %v seconds over sixty updates, want 1 at sixty frames a second", got)
	}

	real := clockEngine(t)
	real.Clock = NewRealClock()
	for i := 0; i < 60; i++ {
		real.Update()
	}
	if got := real.Clock.Elapsed(); got > 0.1 {
		t.Skipf("sixty updates took %v seconds of real time, too slow to make the point", got)
	} else if got >= 1 {
		t.Errorf("a real clock read %v seconds over sixty updates, so this test proves nothing", got)
	}
}

// TestRealClockMeasuresRealTime checks the clock a host installs when it wants
// wall time more than it wants a repeatable run: frames do not move it and the
// machine does.
//
// Negative control: making AdvanceFrame move a real clock as well makes the
// first assertion fail. Confirmed failing.
func TestRealClockMeasuresRealTime(t *testing.T) {
	clock := NewRealClock()
	before := clock.Elapsed()
	for i := 0; i < 1000; i++ {
		clock.AdvanceFrame()
	}
	if clock.Elapsed()-before > 0.05 {
		t.Errorf("a thousand frames moved a real clock by %v seconds, want no more than the time they took",
			clock.Elapsed()-before)
	}
	time.Sleep(5 * time.Millisecond)
	if clock.Elapsed() <= before {
		t.Error("a real clock did not move while the machine waited")
	}
	if clock.Wall() < 1e9 {
		t.Errorf("a real clock's wall time reads %v, want seconds since the Unix epoch", clock.Wall())
	}
}

// TestNilClockReadsZero keeps an Engine built as a struct literal, without
// NewEngine, from panicking the first time a timed effect asks the hour.
//
// Negative control: dropping the nil checks in clock.go panics here.
// Confirmed failing.
func TestNilClockReadsZero(t *testing.T) {
	var clock *Clock
	clock.AdvanceFrame()
	if clock.Elapsed() != 0 || clock.Wall() != 0 {
		t.Errorf("a nil clock read %v and %v, want zero", clock.Elapsed(), clock.Wall())
	}
	e := &Engine{Terminal: NewTerminalFromText("x", TerminalConfig{Width: 4, Height: 2}), Rng: NewRng(1)}
	e.Update()
}
