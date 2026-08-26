package tuiffects

import "testing"

func poolEngine(t *testing.T, seed int) *Engine {
	t.Helper()
	terminal := NewTerminalFromText("particles", TerminalConfig{Width: 20, Height: 6})
	return NewEngine(terminal, NewRng(uint64(seed)))
}

// glowInitializer gives a particle a one-frame scene called "glow" and counts
// how many particles it was run on.
func glowInitializer(runs *int) func(*Engine, *Character) {
	return func(_ *Engine, ch *Character) {
		*runs++
		scene := ch.Animation.NewScene("glow", SceneOptions{Frames: 1})
		if err := scene.AddFrame("*", 1, VisualParams{}); err != nil {
			panic(err)
		}
	}
}

// TestParticleResetZeroValueIsUpstreamDefault is the reason three of
// ParticleReset's fields are inverted against upstream's names.
//
// Go zeroes a struct, upstream defaults three of these to true, and a particle
// handed out still flying along its last path is a bug that shows up as a
// spark that will not fall. The zero value has to mean what upstream's default
// means, and this pins it.
//
// Negative control: renaming the fields to upstream's ClearPaths /
// DeactivatePath / DeactivateScene, so the zero value resets nothing, leaves
// the path in place and this fails. Confirmed failing.
func TestParticleResetZeroValueIsUpstreamDefault(t *testing.T) {
	e := poolEngine(t, 1)
	pool, err := NewParticlePool([]string{"*"}, 0, Coord{}, glowInitializer(new(int)))
	if err != nil {
		t.Fatalf("building the pool: %v", err)
	}
	particle := pool.Acquire(e, "", ParticleReset{})
	if particle == nil {
		t.Fatal("the pool handed out nothing")
	}

	path, err := particle.Motion.NewPath("fall", PathOptions{Speed: 1})
	if err != nil {
		t.Fatalf("building a path: %v", err)
	}
	if _, err := path.NewWaypoint(C(5, 1), nil, ""); err != nil {
		t.Fatalf("adding a waypoint: %v", err)
	}
	e.ActivatePath(particle, "fall")
	e.ActivateScene(particle, "glow")
	pool.Reclaim(e, particle, true, false)

	reused := pool.Acquire(e, "", ParticleReset{})
	if reused != particle {
		t.Fatalf("the pool handed out a different particle, so nothing was tested")
	}
	if !reused.Motion.MovementIsComplete() {
		t.Error("the reused particle is still travelling: the zero value did not stop its path")
	}
	if reused.Motion.Path("fall") != nil {
		t.Error("the reused particle still has its old path: the zero value did not clear the path table")
	}
	if !reused.Animation.ActiveSceneIsComplete() {
		t.Error("the reused particle still has a scene running: the zero value did not stop it")
	}
	if reused.Animation.Scene("glow") == nil {
		t.Error("the reused particle lost its scenes: the zero value must keep them")
	}
}

// TestParticlePoolReusesReclaimedParticles is what the pool is for. A thousand
// sparks a second must be a thousand reuses, not a thousand new characters
// that live on the terminal until the run ends.
//
// Negative control: making Acquire always create rather than pop the free list
// makes the pool grow to four and this fails. Confirmed failing.
func TestParticlePoolReusesReclaimedParticles(t *testing.T) {
	e := poolEngine(t, 2)
	pool, err := NewParticlePool([]string{"*"}, 0, Coord{}, nil)
	if err != nil {
		t.Fatalf("building the pool: %v", err)
	}
	if err := pool.Preallocate(e, 2); err != nil {
		t.Fatalf("preallocating: %v", err)
	}
	charactersAfterPreallocate := len(e.Terminal.Characters)

	first := pool.Emit(e, C(3, 3), "", true, ParticleReset{}, nil)
	second := pool.Emit(e, C(4, 3), "", true, ParticleReset{}, nil)
	if first == nil || second == nil {
		t.Fatal("the pool ran out with two particles preallocated")
	}
	pool.Reclaim(e, first, true, true)
	pool.Reclaim(e, second, true, true)

	third := pool.Emit(e, C(5, 3), "", true, ParticleReset{}, nil)
	fourth := pool.Emit(e, C(6, 3), "", true, ParticleReset{}, nil)
	if third == nil || fourth == nil {
		t.Fatal("the pool ran out after both particles were reclaimed")
	}
	if pool.Len() != 2 {
		t.Errorf("the pool owns %d particles after four emissions, want 2", pool.Len())
	}
	if len(e.Terminal.Characters) != charactersAfterPreallocate {
		t.Errorf("the terminal grew from %d characters to %d, so emitting built new ones",
			charactersAfterPreallocate, len(e.Terminal.Characters))
	}
}

// TestParticlePoolReclaimIsIdempotent covers the case an effect hits by
// accident: a particle whose path and scene both finish on the same frame, or
// which has two handlers registered from two emissions, is reclaimed twice. If
// the second reclaim queued it again, the pool would hand the same character
// to two callers at once and one of them would silently drive the other's
// spark.
//
// Negative control: dropping the "already queued" check in push hands the same
// character out twice and this fails. Confirmed failing.
func TestParticlePoolReclaimIsIdempotent(t *testing.T) {
	e := poolEngine(t, 3)
	pool, err := NewParticlePool([]string{"*"}, 0, Coord{}, nil)
	if err != nil {
		t.Fatalf("building the pool: %v", err)
	}
	particle := pool.Emit(e, C(3, 3), "", true, ParticleReset{}, nil)
	if particle == nil {
		t.Fatal("the pool handed out nothing")
	}
	pool.Reclaim(e, particle, true, true)
	pool.Reclaim(e, particle, true, true)
	if got := pool.AvailableCount(); got != 1 {
		t.Fatalf("the free list holds %d entries after two reclaims of one particle, want 1", got)
	}

	again := pool.Acquire(e, "", ParticleReset{})
	andAgain := pool.Acquire(e, "", ParticleReset{})
	if again == andAgain {
		t.Error("the pool handed the same character out twice at once")
	}
	if pool.Len() != 2 {
		t.Errorf("the pool owns %d particles, want 2: the second acquire should have built one", pool.Len())
	}
}

// TestParticlePoolRespectsMaxSize checks the cap. burn and thunderstorm both
// set one, because a storm that never stops raining would otherwise add a
// character per raindrop for as long as it ran.
//
// Negative control: ignoring MaxSize in Acquire returns a fourth particle and
// this fails. Confirmed failing.
func TestParticlePoolRespectsMaxSize(t *testing.T) {
	e := poolEngine(t, 4)
	pool, err := NewParticlePool([]string{"*"}, 3, Coord{}, nil)
	if err != nil {
		t.Fatalf("building the pool: %v", err)
	}
	for i := 0; i < 3; i++ {
		if pool.Emit(e, C(3, 3), "", true, ParticleReset{}, nil) == nil {
			t.Fatalf("emission %d returned nothing while the cap was 3", i)
		}
	}
	if pool.Emit(e, C(3, 3), "", true, ParticleReset{}, nil) != nil {
		t.Error("the pool went past its cap of 3")
	}
	if pool.Len() != 3 {
		t.Errorf("the pool owns %d particles, want 3", pool.Len())
	}
	if err := pool.Preallocate(e, 4); err == nil {
		t.Error("preallocating past the cap was allowed")
	}
}

// TestParticlePoolInitializerRunsOncePerParticle pins the reason the pool
// holds the initializer rather than taking it per call. The initializer builds
// the scenes a particle needs for its whole life; running it again on reuse
// would rebuild them every emission, which is the cost the pool exists to
// avoid, and would leave a growing pile of scenes on each particle.
//
// Negative control: running the initializer in Acquire's reuse branch as well
// takes the count to five and this fails. Confirmed failing.
func TestParticlePoolInitializerRunsOncePerParticle(t *testing.T) {
	e := poolEngine(t, 5)
	runs := 0
	pool, err := NewParticlePool([]string{"*"}, 0, Coord{}, glowInitializer(&runs))
	if err != nil {
		t.Fatalf("building the pool: %v", err)
	}
	if err := pool.Preallocate(e, 2); err != nil {
		t.Fatalf("preallocating: %v", err)
	}
	if runs != 2 {
		t.Fatalf("the initializer ran %d times over 2 preallocated particles, want 2", runs)
	}
	for i := 0; i < 5; i++ {
		particle := pool.Emit(e, C(3, 3), "", true, ParticleReset{}, nil)
		if particle == nil {
			t.Fatalf("emission %d returned nothing", i)
		}
		pool.Reclaim(e, particle, true, true)
	}
	if runs != 2 {
		t.Errorf("the initializer ran %d times, want 2: it must not run on a reused particle", runs)
	}
}

// TestParticlePoolEmitPlacesShowsAndActivates covers Emit's four jobs, in
// order. onEmit runs after the particle is placed and before it is shown, so
// the path it builds starts from the right cell and no frame draws the
// particle at its previous position.
//
// Negative control: skipping the Activate call leaves the engine with nothing
// to tick and this fails on the active count. Confirmed failing.
func TestParticlePoolEmitPlacesShowsAndActivates(t *testing.T) {
	e := poolEngine(t, 6)
	pool, err := NewParticlePool([]string{"*"}, 0, Coord{}, nil)
	if err != nil {
		t.Fatalf("building the pool: %v", err)
	}
	origin := C(7, 4)
	seenAt := Coord{}
	seenVisible := true
	particle := pool.Emit(e, origin, "@", true, ParticleReset{}, func(_ *Engine, ch *Character) {
		seenAt = ch.Motion.CurrentCoord
		seenVisible = ch.IsVisible
	})
	if particle == nil {
		t.Fatal("the pool handed out nothing")
	}
	if seenAt != origin {
		t.Errorf("onEmit saw the particle at %v, want it already placed at %v", seenAt, origin)
	}
	if seenVisible {
		t.Error("onEmit saw the particle already visible, want it shown only after onEmit ran")
	}
	if !particle.IsVisible {
		t.Error("the particle is not visible after Emit")
	}
	if particle.InputSymbol != "@" {
		t.Errorf("the particle carries %q, want the symbol Emit was given", particle.InputSymbol)
	}
	if e.ActiveCount() != 1 {
		t.Errorf("the engine has %d active characters after one emission, want 1", e.ActiveCount())
	}
}

// TestReclaimOnEventReturnsTheParticle covers the wiring every effect uses:
// the particle puts itself back when the scene it was emitted for finishes,
// with no per-frame bookkeeping in the effect.
//
// Negative control: registering nothing leaves the particle visible and off
// the free list and this fails. Confirmed failing.
func TestReclaimOnEventReturnsTheParticle(t *testing.T) {
	e := poolEngine(t, 7)
	pool, err := NewParticlePool([]string{"*"}, 0, Coord{}, glowInitializer(new(int)))
	if err != nil {
		t.Fatalf("building the pool: %v", err)
	}
	if err := pool.Preallocate(e, 1); err != nil {
		t.Fatalf("preallocating: %v", err)
	}
	pool.ReclaimOnEvent(pool.Particles[0], SceneComplete, SceneCaller("glow"), true, true)

	particle := pool.Emit(e, C(3, 3), "", true, ParticleReset{}, func(e *Engine, ch *Character) {
		e.ActivateScene(ch, "glow")
	})
	if particle == nil {
		t.Fatal("the pool handed out nothing")
	}
	if pool.AvailableCount() != 0 {
		t.Fatal("the emitted particle is still on the free list")
	}

	e.Update()
	if pool.AvailableCount() != 1 {
		t.Errorf("the free list holds %d particles after the scene finished, want 1", pool.AvailableCount())
	}
	if particle.IsVisible {
		t.Error("the reclaimed particle is still visible")
	}
	if e.ActiveCount() != 0 {
		t.Errorf("the engine still has %d active characters, want 0", e.ActiveCount())
	}
}
