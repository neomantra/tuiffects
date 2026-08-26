package tuiffects

import "errors"

// ParticlePool recycles characters that are not part of the input: sparks,
// smoke, raindrops. An effect that throws off thousands of short-lived
// characters would otherwise allocate a thousand more every second and never
// free any of them, because a character lives on the terminal for the whole
// run.
//
// A particle goes round a loop. Emit takes one off the free list, puts it
// where the effect wants it, lets the effect give it a path and a scene, shows
// it and makes it active. When it is finished, Reclaim hides it and puts it
// back on the free list. Reclaim usually runs from an event: ReclaimOnEvent
// wires it to the scene or path whose completion means the particle is spent.
//
//	pool, err := NewParticlePool([]string{"*", "."}, 2000, Coord{}, initSpark)
//	pool.Preallocate(e, 2000)
//	...
//	spark := pool.Emit(e, origin, "", true, ParticleReset{}, func(e *Engine, ch *Character) {
//	    // give it a path and a scene, then activate them
//	})
//
// Ported from ttfx src/engine/particles.rs.
type ParticlePool struct {
	// Symbols is what a new particle can look like. Emit picks one at random
	// unless it is given a symbol.
	Symbols []string
	// MaxSize caps how many particles the pool will ever create. Zero means
	// no cap. Once the cap is reached and the free list is empty, Emit
	// returns nil rather than growing.
	MaxSize int
	// Coord is where a newly created particle is placed before anything moves
	// it. Emit moves it, so this only matters for the frame a particle is
	// made in.
	Coord Coord
	// Particles is every particle the pool owns, free or in flight, in the
	// order they were created. An effect that has to register something on
	// each of them walks this.
	Particles []*Character

	// available is the free list. It is a stack: the particle reclaimed most
	// recently is the next one handed out, matching upstream's deque, which
	// pushes and pops the same end.
	available []*Character
	// inPool mirrors available, so reclaiming a particle twice does not put
	// it on the free list twice and hand the same character to two callers.
	inPool map[*Character]struct{}

	initializer func(e *Engine, ch *Character)
}

// ParticleReset says how much of a particle's previous life to wipe when it is
// handed out again.
//
// The zero value is upstream's default, and it is what every effect in the
// catalogue uses: the particle's paths are thrown away and its running path
// and scene are stopped, while its scenes are kept because building them again
// per emission is the cost the pool exists to avoid.
//
// Three of these fields are inverted against upstream, which names them
// clear_paths, deactivate_path and deactivate_scene and defaults all three to
// true. A Go struct defaults to false, so a faithful set of names would make
// the zero value mean "reset nothing" and quietly hand out a particle still
// flying along its last path. Inverting them puts upstream's default on the
// zero value, and asking for the non-default now has to be written down.
type ParticleReset struct {
	// KeepPaths leaves the particle's paths in place. Upstream's
	// clear_paths=False.
	KeepPaths bool
	// ClearScenes throws the particle's scenes away as well. Upstream's
	// clear_scenes=True. An effect that sets this pays to rebuild the scenes
	// on every emission.
	ClearScenes bool
	// ClearEvents drops the actions registered on the particle. Upstream's
	// clear_events=True. Set it when the effect registers a fresh handler per
	// emission, or they accumulate over the run.
	ClearEvents bool
	// KeepActivePath leaves the particle travelling. Upstream's
	// deactivate_path=False.
	KeepActivePath bool
	// KeepActiveScene leaves the particle's scene running. Upstream's
	// deactivate_scene=False.
	KeepActiveScene bool
	// ResetAppearance puts the particle back to its input symbol with no
	// colours. Upstream's reset_appearance=True.
	ResetAppearance bool
}

// ErrNoParticleSymbols is returned when a pool is built with nothing to draw.
var ErrNoParticleSymbols = errors.New("tuiffects: a particle pool needs at least one symbol")

// ErrParticleCountAboveMax is returned when Preallocate is asked for more
// particles than MaxSize allows.
var ErrParticleCountAboveMax = errors.New("tuiffects: cannot preallocate more particles than the pool's maximum")

// NewParticlePool builds a pool. maxSize of zero means no cap.
//
// initializer runs once on each particle the pool creates, and never on one it
// reuses. It is where the effect builds the scenes a particle needs for its
// whole life, so that emitting one is cheap. It may be nil.
//
// Upstream passes the initializer to every call that might create a particle
// instead of holding it. Every effect in the catalogue passes the same closure
// to all of them, and passing two different ones would mean particles that
// behave differently depending on whether the pool happened to be empty, so
// this holds it once.
func NewParticlePool(symbols []string, maxSize int, coord Coord, initializer func(e *Engine, ch *Character)) (*ParticlePool, error) {
	if len(symbols) == 0 {
		return nil, ErrNoParticleSymbols
	}
	return &ParticlePool{
		Symbols:     symbols,
		MaxSize:     maxSize,
		Coord:       coord,
		inPool:      make(map[*Character]struct{}),
		initializer: initializer,
	}, nil
}

// Preallocate creates count particles up front and puts them on the free list,
// so the first burst of emissions does not build characters mid-frame.
func (p *ParticlePool) Preallocate(e *Engine, count int) error {
	if p.MaxSize > 0 && p.MaxSize < count {
		return ErrParticleCountAboveMax
	}
	for i := 0; i < count; i++ {
		p.push(p.createParticle(e, ""))
	}
	return nil
}

// Len is how many particles the pool owns in total, free and in flight.
func (p *ParticlePool) Len() int { return len(p.Particles) }

// AvailableCount is how many particles are on the free list.
func (p *ParticlePool) AvailableCount() int { return len(p.available) }

func (p *ParticlePool) push(ch *Character) {
	if _, queued := p.inPool[ch]; queued {
		return
	}
	p.available = append(p.available, ch)
	p.inPool[ch] = struct{}{}
}

// createParticle is upstream's _create_particle: a fresh character carrying a
// symbol from the pool, run through the initializer.
func (p *ParticlePool) createParticle(e *Engine, symbol string) *Character {
	if symbol == "" {
		symbol = *Choice(e.Rng, p.Symbols)
	}
	ch := e.Terminal.AddCharacter(symbol, p.Coord)
	if p.initializer != nil {
		p.initializer(e, ch)
	}
	p.Particles = append(p.Particles, ch)
	return ch
}

// resetParticle is upstream's _reset_particle.
func (p *ParticlePool) resetParticle(e *Engine, ch *Character, reset ParticleReset) {
	if !reset.KeepActivePath {
		ch.Motion.DeactivatePath("")
	}
	if !reset.KeepActiveScene {
		e.DeactivateScene(ch, "")
	}
	if !reset.KeepPaths {
		ch.Motion.ClearPaths()
	}
	if reset.ClearScenes {
		ch.Animation.ClearScenes()
	}
	if reset.ClearEvents {
		ch.ClearEvents()
	}
	if reset.ResetAppearance {
		ch.Animation.SetAppearance(ch.InputSymbol, ColorPair{}, ch.UsesInputColors)
	}
}

// Acquire takes a particle off the free list, or creates one if the free list
// is empty and the cap allows. It returns nil when the pool is capped out.
//
// An empty symbol keeps whatever the particle already looks like, which for a
// new one is a symbol drawn at random from Symbols. A given symbol replaces it.
//
// The particle is not placed, shown or activated: Acquire is for an effect
// that wants to do all of that itself. Emit is the usual call.
func (p *ParticlePool) Acquire(e *Engine, symbol string, reset ParticleReset) *Character {
	if n := len(p.available); n > 0 {
		ch := p.available[n-1]
		p.available = p.available[:n-1]
		delete(p.inPool, ch)
		p.resetParticle(e, ch, reset)
		if symbol != "" {
			ch.InputSymbol = symbol
			ch.Animation.SetAppearance(symbol, ColorPair{}, ch.UsesInputColors)
		}
		return ch
	}
	if p.MaxSize > 0 && len(p.Particles) >= p.MaxSize {
		return nil
	}
	ch := p.createParticle(e, symbol)
	p.resetParticle(e, ch, reset)
	return ch
}

// Emit acquires a particle, puts it at origin, hands it to onEmit, then shows
// it and makes it active. It returns nil when the pool is capped out and the
// free list is empty, which an effect throwing off decoration can ignore.
//
// onEmit is where the effect gives the particle the path and scene for this
// flight and activates them. It may be nil.
func (p *ParticlePool) Emit(e *Engine, origin Coord, symbol string, visible bool, reset ParticleReset, onEmit func(e *Engine, ch *Character)) *Character {
	ch := p.Acquire(e, symbol, reset)
	if ch == nil {
		return nil
	}
	ch.Motion.SetCoordinate(origin)
	if onEmit != nil {
		onEmit(e, ch)
	}
	e.Terminal.SetCharacterVisibility(ch, visible)
	e.Activate(ch)
	return ch
}

// Reclaim puts a particle back on the free list. Reclaiming one that is
// already there does nothing, so an effect may call it from more handlers than
// will actually fire.
func (p *ParticlePool) Reclaim(e *Engine, ch *Character, hide, deactivate bool) {
	if hide {
		e.Terminal.SetCharacterVisibility(ch, false)
	}
	if deactivate {
		ch.Motion.DeactivatePath("")
		e.DeactivateScene(ch, "")
	}
	e.Deactivate(ch)
	p.push(ch)
}

// ReclaimOnEvent registers Reclaim against an event, which is how a particle
// puts itself back once its scene or path finishes.
//
//	pool.ReclaimOnEvent(spark, SceneComplete, SceneCaller("glow"), true, true)
//
// Registering it again on the same particle and event is harmless: reclaiming
// twice does nothing the second time. Upstream reaches this through a callback
// id table because Rust cannot hold the closure; here it is the closure.
func (p *ParticlePool) ReclaimOnEvent(ch *Character, event Event, from Caller, hide, deactivate bool) {
	ch.RegisterEvent(event, from, Callback(func(e *Engine, particle *Character) {
		p.Reclaim(e, particle, hide, deactivate)
	}))
}

// Extend adopts characters the effect made itself. They join the free list as
// they are, with no reset, so whatever the effect set up on them survives.
func (p *ParticlePool) Extend(particles ...*Character) {
	for _, ch := range particles {
		p.Particles = append(p.Particles, ch)
		p.push(ch)
	}
}
