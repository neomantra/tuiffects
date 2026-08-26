package tuiffects

import "strconv"

// rings, ported from ttfx src/effects/rings.rs, which ports
// TerminalTextEffects effects/effect_rings.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "rings",
		Description: "Characters form spinning rings, scatter, and return to the text",
		New:         func() Effect { return NewRings(DefaultRingsConfig()) },
	})
}

// RingsConfig tunes the rings effect.
type RingsConfig struct {
	// RingColors are cycled through as the rings are built, so ring 0 takes
	// the first colour, ring 1 the second, and so on.
	RingColors []Color
	// RingGap is the distance between rings, as a share of the smaller canvas
	// dimension. It is also how far a character wanders while dispersed.
	RingGap float64
	// SpinDuration is how many frames each spinning stretch lasts.
	SpinDuration int
	// SpinSpeedLow and SpinSpeedHigh bound a ring's rotation speed. Each ring
	// draws its own speed from the range, which is what stops the rings
	// turning as one disc.
	SpinSpeedLow  float64
	SpinSpeedHigh float64
	// DisperseDuration is how many frames the characters spend scattered
	// between spinning stretches.
	DisperseDuration int
	// SpinDisperseCycles is how many times the effect spins and scatters
	// before the text goes home.
	SpinDisperseCycles int
	// FinalGradientStops colour the text once it settles. They are ignored
	// when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultRingsConfig is upstream's default rings.
func DefaultRingsConfig() RingsConfig {
	stops := []Color{
		MustParseColor("ab48ff"), MustParseColor("e7b2b2"), MustParseColor("fffebd"),
	}
	return RingsConfig{
		RingColors:         append([]Color(nil), stops...),
		RingGap:            0.1,
		SpinDuration:       200,
		SpinSpeedLow:       0.25,
		SpinSpeedHigh:      1.0,
		DisperseDuration:   200,
		SpinDisperseCycles: 3,

		FinalGradientStops:     append([]Color(nil), stops...),
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// The constants upstream writes inline. Nothing derives them.
const (
	// ringsOpeningFrames is how long the untouched text sits on screen before
	// anything moves.
	ringsOpeningFrames = 100
	// ringsCoordsPerRadius is how many points a ring of radius r is sampled
	// at: 7r, before duplicates are dropped.
	ringsCoordsPerRadius = 7
	// ringsMinimumInCanvas is the share of a ring that must fall inside the
	// canvas for the ring to be worth building. The first ring that fails
	// stops the loop.
	ringsMinimumInCanvas = 0.25
	// ringsDisperseWaypoints is how many places a scattered character
	// wanders between, on a loop, while it is dispersed.
	ringsDisperseWaypoints = 5
	// ringsRingGradientSteps is the length of the ramp between a character's
	// final colour and its ring's colour.
	ringsRingGradientSteps = 8

	ringsDisperseSpeed        = 0.14
	ringsInitialDisperseSpeed = 0.3
	ringsCondenseSpeed        = 0.1
	ringsHomeSpeed            = 0.8
	ringsExternalSpeed        = 0.8
)

// ringsPhase is upstream's phase string, which drives the whole effect.
type ringsPhase int

const (
	ringsPhaseStart ringsPhase = iota
	ringsPhaseDisperse
	ringsPhaseSpin
	ringsPhaseFinal
	ringsPhaseComplete
)

// ringsRing is one ring of characters: the circle of cells it turns around,
// the colour it wears, and the characters riding it.
type ringsRing struct {
	radius int
	origin Coord

	// counterClockwise is the circle in the order the sampler produced it and
	// clockwise is the same circle reversed. A ring uses one or the other, so
	// neighbouring rings turn opposite ways.
	counterClockwise []Coord
	clockwise        []Coord

	ringGap   int
	ringColor Color

	characters []*Character
	// lastRingPath is where on its ring each character was when the ring last
	// scattered, so the ring can put it back on the same cell. Upstream holds
	// Path objects here and compares them by id, so ids are what is kept.
	lastRingPath map[*Character]string

	rotationSpeed float64
}

// newRingsRing builds a ring and draws its rotation speed.
func newRingsRing(e *Engine, config RingsConfig, radius int, origin Coord, coords []Coord, ringGap int, ringColor Color) *ringsRing {
	clockwise := make([]Coord, len(coords))
	for i, coord := range coords {
		clockwise[len(coords)-1-i] = coord
	}
	return &ringsRing{
		radius:           radius,
		origin:           origin,
		counterClockwise: coords,
		clockwise:        clockwise,
		ringGap:          ringGap,
		ringColor:        ringColor,
		lastRingPath:     make(map[*Character]string),
		rotationSpeed:    e.Rng.Uniform(config.SpinSpeedLow, config.SpinSpeedHigh),
	}
}

// Rings gathers the text into concentric spinning rings, scatters it, spins it
// again, and finally walks every character home.
//
// This effect neither passes over the screen nor assembles it: it starts from
// the assembled picture and takes it apart. Upstream already shows every
// character in place, in its final colour, on the first frame, because the
// rings have to form out of text the viewer has seen. That is exactly what
// DynamicExistingColors needs, so nothing here is scoped to the colour policy
// beyond the colours themselves.
type Rings struct {
	config RingsConfig

	rings        []*ringsRing
	nonRingChars []*Character
	finalColors  map[*Character]ColorPair

	phase                   ringsPhase
	initialDisperseComplete bool
	spinTimeRemaining       int
	disperseTimeRemaining   int
	cyclesRemaining         int
	openingFramesRemaining  int
	// disperseRound names each round of disperse paths. See
	// makeDisperseWaypoints for why the paths are not reused.
	disperseRound int
}

// NewRings builds the effect.
func NewRings(config RingsConfig) *Rings {
	return &Rings{
		config:                 config,
		finalColors:            make(map[*Character]ColorPair),
		phase:                  ringsPhaseStart,
		spinTimeRemaining:      config.SpinDuration,
		disperseTimeRemaining:  config.DisperseDuration,
		cyclesRemaining:        config.SpinDisperseCycles,
		openingFramesRemaining: ringsOpeningFrames,
	}
}

// Build shows the text, lays out the rings, and hands every character either a
// place on a ring or a one-way trip off the edge of the canvas.
func (r *Rings) Build(e *Engine) error {
	canvas := e.Terminal.Canvas
	ringGap := max(roundHalfEven(float64(min(canvas.Top, canvas.Right))*r.config.RingGap), 1)

	finalGradient, err := NewGradient(r.config.FinalGradientStops, r.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		r.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)
	pending := make([]*Character, 0, len(characters))
	for _, ch := range characters {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			// May be the empty pair, which is one frame of nothing: the
			// character comes home as the terminal default it arrived as.
			final = ch.Animation.InputColors
		}
		r.finalColors[ch] = final

		start := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors, Frames: 1})
		if err := start.AddFrame(ch.InputSymbol, 1, VisualParams{Colors: final}); err != nil {
			return err
		}

		home, err := ch.Motion.NewPath("home", PathOptions{
			Speed:   ringsHomeSpeed,
			Ease:    OutQuad,
			HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := home.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
			return err
		}

		e.ActivateScene(ch, start.ID)
		e.Terminal.SetCharacterVisibility(ch, true)
		pending = append(pending, ch)
	}

	// The rings are filled in a shuffled order, so a ring is drawn from all
	// over the text rather than from one corner of it.
	Shuffle(e.Rng, pending)

	radiusLimit := max(canvas.Right, canvas.Top)
	for radius := 1; radius < radiusLimit; radius += ringGap {
		coords := FindCoordsOnCircle(canvas.Center, radius, ringsCoordsPerRadius*radius, true)
		if len(coords) == 0 {
			break
		}
		inCanvas := 0
		for _, coord := range coords {
			if canvas.CoordIsInCanvas(coord) {
				inCanvas++
			}
		}
		// Once most of a ring hangs off the canvas there is no point in the
		// rings beyond it either, so the loop stops rather than skipping.
		if float64(inCanvas)/float64(len(coords)) < ringsMinimumInCanvas {
			break
		}
		color := r.config.RingColors[len(r.rings)%len(r.config.RingColors)]
		r.rings = append(r.rings, newRingsRing(e, r.config, radius, canvas.Center, coords, ringGap, color))
	}

	onRing := make(map[*Character]bool, len(pending))
	next := 0
	for index, ring := range r.rings {
		// Rings alternate direction, so neighbouring rings shear against each
		// other instead of turning as one disc.
		clockwise := index%2 == 1
		for range ring.counterClockwise {
			if next >= len(pending) {
				break
			}
			ch := pending[next]
			next++
			if err := r.addCharacterToRing(e, ring, ch, clockwise, dynamic); err != nil {
				return err
			}
			onRing[ch] = true
		}
	}

	// Whatever the rings could not hold is thrown off the canvas and hidden.
	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		if onRing[ch] {
			continue
		}
		external, err := ch.Motion.NewPath("external", PathOptions{
			Speed:   ringsExternalSpeed,
			Ease:    OutSine,
			HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := external.NewWaypoint(canvas.RandomCoord(e.Rng, true, false), nil, ""); err != nil {
			return err
		}
		r.nonRingChars = append(r.nonRingChars, ch)
		ch.RegisterEvent(PathComplete, PathCaller("external"), Callback(ringsHide))
	}
	return nil
}

// ringsHide is the callback upstream routes through a callback id table.
func ringsHide(e *Engine, ch *Character) { e.Terminal.SetCharacterVisibility(ch, false) }

// addCharacterToRing gives a character its place on a ring: the ramp into the
// ring colour, one single-waypoint path per cell of the ring chained into a
// loop, and the ramp back out again.
func (r *Rings) addCharacterToRing(e *Engine, ring *ringsRing, ch *Character, clockwise, dynamic bool) error {
	// The dynamic branch resolves to the colours the character arrived with,
	// so there is no ramp to build and one frame says everything.
	useInput := dynamic
	final := r.finalColors[ch]

	frames := ringsRingGradientSteps + 1
	if useInput {
		frames = 1
	}

	gradient := ch.Animation.NewScene("gradient", SceneOptions{
		UsesInputColors: ch.UsesInputColors,
		Frames:          frames,
	})
	if useInput {
		if err := gradient.AddFrame(ch.InputSymbol, 1, VisualParams{Colors: final}); err != nil {
			return err
		}
	} else {
		ramp, err := NewGradientSteps([]Color{final.Fg, ring.ringColor}, ringsRingGradientSteps, false)
		if err != nil {
			return err
		}
		if err := gradient.ApplyGradientToSymbols([]string{ch.InputSymbol}, 3, ramp, nil); err != nil {
			return err
		}
	}

	// The ring is rotated so that this character's first waypoint is the cell
	// it joins at. Every character then walks the whole ring from there.
	coords := ring.counterClockwise
	if clockwise {
		coords = ring.clockwise
	}
	startIndex := len(ring.characters)
	ringPaths := make([]string, 0, len(coords))
	for i := range coords {
		coord := coords[(startIndex+i)%len(coords)]
		path, err := ch.Motion.NewPath(strconv.Itoa(len(ringPaths)), PathOptions{Speed: ring.rotationSpeed})
		if err != nil {
			return err
		}
		if _, err := path.NewWaypoint(coord, nil, strconv.Itoa(len(path.Waypoints))); err != nil {
			return err
		}
		ringPaths = append(ringPaths, path.ID)
	}
	if len(ringPaths) == 0 {
		return nil
	}
	ring.lastRingPath[ch] = ringPaths[0]

	disperse := ch.Animation.NewScene("disperse", SceneOptions{
		UsesInputColors: ch.UsesInputColors,
		Frames:          frames,
	})
	if useInput {
		if err := disperse.AddFrame(ch.InputSymbol, 1, VisualParams{Colors: final}); err != nil {
			return err
		}
	} else {
		ramp, err := NewGradientSteps([]Color{ring.ringColor, final.Fg}, ringsRingGradientSteps, false)
		if err != nil {
			return err
		}
		if err := disperse.ApplyGradientToSymbols([]string{ch.InputSymbol}, 10, ramp, nil); err != nil {
			return err
		}
	}

	e.ChainPaths(ch, ringPaths, true)
	ring.characters = append(ring.characters, ch)
	return nil
}

// makeDisperseWaypoints gives a character a looping wander around origin, one
// ring gap wide, and returns the path id.
//
// Upstream deletes the character's previous disperse path and rebuilds it
// under the same name. Nothing here removes a path, and NewPath rejects a
// duplicate id, so each round of scattering gets its own name instead. The
// abandoned paths are never activated again and carry no handlers; they cost
// one entry each in the character's path table.
func (r *Rings) makeDisperseWaypoints(e *Engine, ring *ringsRing, ch *Character, origin Coord) (string, error) {
	options := FindCoordsInRect(origin, ring.ringGap)
	path, err := ch.Motion.NewPath("disperse"+strconv.Itoa(r.disperseRound), PathOptions{
		Speed: ringsDisperseSpeed,
		Loop:  true,
	})
	if err != nil {
		return "", err
	}
	for i := 0; i < ringsDisperseWaypoints; i++ {
		coord := origin
		if len(options) > 0 {
			coord = options[e.Rng.IntBelow(0, len(options))]
		}
		if _, err := path.NewWaypoint(coord, nil, ""); err != nil {
			return "", err
		}
	}
	return path.ID, nil
}

// startInitialDisperse throws the rings apart for the first time. It is
// separate from disperseRing because the characters are still sitting in the
// text and have to be flown to the ring's scatter field first.
func (r *Rings) startInitialDisperse(e *Engine) error {
	for _, ring := range r.rings {
		for _, ch := range ring.characters {
			ringStart := ch.Motion.Path("0")
			if ringStart == nil || len(ringStart.Waypoints) == 0 {
				continue
			}
			dispersePath, err := r.makeDisperseWaypoints(e, ring, ch, ringStart.Waypoints[0].Coord)
			if err != nil {
				return err
			}
			target := ch.Motion.Path(dispersePath).Waypoints[0].Coord
			initial, err := ch.Motion.NewPath("", PathOptions{
				Speed:   ringsInitialDisperseSpeed,
				Ease:    OutCubic,
				HasEase: true,
			})
			if err != nil {
				return err
			}
			if _, err := initial.NewWaypoint(target, nil, ""); err != nil {
				return err
			}
			ch.RegisterEvent(PathComplete, PathCaller(initial.ID), ActivatePath(dispersePath))
			e.ActivateScene(ch, "disperse")
			e.ActivatePath(ch, initial.ID)
			e.Activate(ch)
		}
	}
	r.disperseRound++

	for _, ch := range r.nonRingChars {
		e.ActivatePath(ch, "external")
		e.Activate(ch)
	}
	return nil
}

// disperseRing scatters a ring that is already spinning. Each character starts
// its wander from wherever on the ring it happens to be.
func (r *Rings) disperseRing(e *Engine, ring *ringsRing) error {
	for _, ch := range ring.characters {
		// Where the character is on its ring right now is the path it is
		// running, which is engine state no exported accessor reads. Upstream
		// falls back to the first ring path when nothing is running.
		last := "0"
		if ch.Motion.hasActivePath {
			last = ch.Motion.activePath
		}
		ring.lastRingPath[ch] = last

		dispersePath, err := r.makeDisperseWaypoints(e, ring, ch, ch.Motion.CurrentCoord)
		if err != nil {
			return err
		}
		e.ActivatePath(ch, dispersePath)
		e.ActivateScene(ch, "disperse")
	}
	return nil
}

// spinRing gathers a scattered ring back onto the circle. Each character is
// walked to the cell it left and picks the chained ring paths up from there.
func (r *Rings) spinRing(e *Engine, ring *ringsRing) error {
	for _, ch := range ring.characters {
		lastRingPath := ring.lastRingPath[ch]
		previous := ch.Motion.Path(lastRingPath)
		if previous == nil || len(previous.Waypoints) == 0 {
			continue
		}
		condense, err := ch.Motion.NewPath("", PathOptions{Speed: ringsCondenseSpeed})
		if err != nil {
			return err
		}
		if _, err := condense.NewWaypoint(previous.Waypoints[0].Coord, nil, ""); err != nil {
			return err
		}
		ch.RegisterEvent(PathComplete, PathCaller(condense.ID), ActivatePath(lastRingPath))
		e.ActivatePath(ch, condense.ID)
		e.ActivateScene(ch, "gradient")
	}
	return nil
}

// goHome ends the effect: everything becomes visible again and walks back to
// where it started, wearing the ramp out of the ring colour on the way.
func (r *Rings) goHome(e *Engine) {
	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		e.Terminal.SetCharacterVisibility(ch, true)
		e.ActivatePath(ch, "home")
		e.Activate(ch)
		if ch.Motion.Path("external") != nil {
			// A character that never made it onto a ring has no ramp to run.
			continue
		}
		e.ActivateScene(ch, "disperse")
	}
}

// Advance runs one frame of whichever phase the effect is in and reports
// whether it is still going.
func (r *Rings) Advance(e *Engine) bool {
	if r.phase == ringsPhaseComplete {
		return false
	}
	switch r.phase {
	case ringsPhaseStart:
		if r.openingFramesRemaining == 0 {
			r.phase = ringsPhaseDisperse
		} else {
			r.openingFramesRemaining--
		}

	case ringsPhaseDisperse:
		switch {
		case !r.initialDisperseComplete:
			r.initialDisperseComplete = true
			if err := r.startInitialDisperse(e); err != nil {
				// Unreachable: every path built here has a positive constant
				// speed and an id no character already holds. Going home
				// resolves the text rather than freezing it mid-flight.
				r.phase = ringsPhaseFinal
				r.goHome(e)
			}
		case r.disperseTimeRemaining == 0:
			r.phase = ringsPhaseSpin
			r.cyclesRemaining--
			r.spinTimeRemaining = r.config.SpinDuration
			for _, ring := range r.rings {
				if err := r.spinRing(e, ring); err != nil {
					r.phase = ringsPhaseFinal
					r.goHome(e)
					break
				}
			}
		default:
			r.disperseTimeRemaining--
		}

	case ringsPhaseSpin:
		switch {
		case r.spinTimeRemaining != 0:
			r.spinTimeRemaining--
		case r.cyclesRemaining == 0:
			r.phase = ringsPhaseFinal
			r.goHome(e)
		default:
			r.disperseTimeRemaining = r.config.DisperseDuration
			for _, ring := range r.rings {
				if err := r.disperseRing(e, ring); err != nil {
					r.phase = ringsPhaseFinal
					r.goHome(e)
					break
				}
			}
			if r.phase == ringsPhaseSpin {
				r.disperseRound++
				r.phase = ringsPhaseDisperse
			}
		}

	case ringsPhaseFinal:
		if e.ActiveCount() == 0 {
			r.phase = ringsPhaseComplete
		}

	case ringsPhaseComplete:
	}

	e.Update()
	return true
}
