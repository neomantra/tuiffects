package tuiffects

// blackhole, ported from ttfx src/effects/blackhole.rs, which ports
// TerminalTextEffects effects/effect_blackhole.py by ChrisBuilds.

import (
	"errors"
	"strconv"
)

// errBlackholeNoStarColors is raised when the singularity has been left with
// no palette to flicker through.
var errBlackholeNoStarColors = errors.New("blackhole: StarColors needs at least one colour")

func init() {
	Register(Descriptor{
		Name:        "blackhole",
		Description: "The text becomes a starfield, a black hole swallows it, then the singularity explodes and puts it back",
		New:         func() Effect { return NewBlackhole(DefaultBlackholeConfig()) },
	})
}

// BlackholeConfig tunes the blackhole effect.
type BlackholeConfig struct {
	// BlackholeColor is the colour of the stars that make up the ring.
	BlackholeColor Color
	// StarColors colour the singularity while it is unstable, in the moment
	// between the ring collapsing and the explosion. They do not colour the
	// explosion itself; see explodeSingularity.
	StarColors []Color
	// FinalGradientStops colour the text once it is back in place. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultBlackholeConfig is upstream's default blackhole.
func DefaultBlackholeConfig() BlackholeConfig {
	return BlackholeConfig{
		BlackholeColor: MustParseColor("ffffff"),
		StarColors: []Color{
			MustParseColor("ffcc0d"), MustParseColor("ff7326"), MustParseColor("ff194d"),
			MustParseColor("bf2669"), MustParseColor("702a8c"), MustParseColor("049dbf"),
		},
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("ffffff"),
		},
		FinalGradientSteps:     []int{9},
		FinalGradientDirection: Diagonal,
	}
}

const (
	// blackholeFormationPathID carries a ring character from wherever it
	// started to its slot on the ring. blackholeSceneID is the star it wears
	// once it is there. Path and scene ids are scoped to one character, so
	// every ring character uses these same names.
	blackholeFormationPathID = "blackhole"
	blackholeSceneID         = "blackhole"
	// blackholeRotationPathID walks the whole ring, so a character on it
	// travels round the circle for as long as the ring holds.
	blackholeRotationPathID = "blackhole_rotation"
	// blackholeSingularityPathID carries a character that is not on the ring
	// into the middle, where it is eaten.
	blackholeSingularityPathID = "singularity"

	// blackholeRingChars is how many characters the ring holds per unit of
	// radius.
	blackholeRingChars = 3
	// blackholeMinRadius is the smallest ring upstream will draw, however
	// small the canvas is.
	blackholeMinRadius = 3
	// blackholeCollapseOvershoot is how far past the ring the characters
	// stretch before they fall into the middle.
	blackholeCollapseOvershoot = 3
	// blackholeExplosionRadius is how far from its home a character is thrown
	// by the explosion before it drifts back.
	blackholeExplosionRadius = 3
	// blackholeExplosionArc is how many landing spots that throw picks from.
	blackholeExplosionArc = 5
	// blackholeUnstableCycles is how many times the singularity runs through
	// its unstable symbols before it goes off.
	blackholeUnstableCycles = 3
	// blackholeCoolingSteps is the length of the ramp from the explosion
	// colour back to the character's resting colour, and blackholeCoolingHold
	// is how long each colour of that ramp is held.
	blackholeCoolingSteps = 10
	blackholeCoolingHold  = 20
	// blackholeFadeSteps is the length of the ramp from a star's colour to
	// black as the black hole eats it.
	blackholeFadeSteps = 10
	// blackholeStarfieldSteps is how many shades of grey the starfield is
	// drawn in.
	blackholeStarfieldSteps = 6
)

// blackholePhase is where the effect has got to. The phases run in order and
// never go back.
type blackholePhase int

const (
	// blackholeForming releases the ring characters one at a time.
	blackholeForming blackholePhase = iota
	// blackholeConsuming sends everything else into the middle.
	blackholeConsuming
	// blackholeCollapsing stretches the ring out and drops it inwards.
	blackholeCollapsing
	// blackholeExploding waits for the singularity to go off and throws the
	// characters back out.
	blackholeExploding
	// blackholeComplete has nothing left to start.
	blackholeComplete
)

// Blackhole turns the text into a starfield, forms a rotating ring of stars,
// pulls every other character into the middle of it, collapses the ring onto
// the same point, and then explodes the point and lets the characters drift
// home.
//
// This effect assembles the picture rather than passing over it. The screen it
// was handed is torn apart in the first frame and only exists again at the
// end, so there is no DynamicExistingColors deviation for visibility here:
// upstream already shows every character from frame one, because at that point
// every character is a star somewhere in the starfield rather than a piece of
// the picture. Revealing the picture in place up front would show the thing
// the effect is about to destroy.
type Blackhole struct {
	config BlackholeConfig

	// blackholeChars are the ring, in the order they were picked, which is
	// also the order they take their slots on the circle.
	blackholeChars []*Character
	// awaitingBlackholeChars is the ring characters that have not been
	// released yet, drained from the front.
	awaitingBlackholeChars []*Character
	// awaitingConsumptionChars is everything else, shuffled, waiting to be
	// pulled in.
	awaitingConsumptionChars []*Character
	// onRing answers "is this character part of the ring" without a scan.
	onRing map[*Character]bool
	// finalColors is where each character settles when the input's own
	// colours are not being used.
	finalColors map[*Character]Color
	// preexistingColors records whether any input character carried a colour.
	// Upstream computes this once when the engine is built and blackhole is
	// the only effect that reads it.
	preexistingColors bool

	blackholeRadius int
	// formationDelay is how many frames sit between two ring characters being
	// released, and frameDelay counts down to the next one.
	formationDelay int
	frameDelay     int

	phase blackholePhase
	err   error
}

// NewBlackhole builds the effect.
func NewBlackhole(config BlackholeConfig) *Blackhole {
	return &Blackhole{config: config, phase: blackholeForming}
}

// Err reports a failure raised after Build returned. The collapse and the
// explosion are built while the effect runs, as they are upstream, so their
// errors cannot be handed back through Build. Advance stops the effect when
// one happens and leaves it here.
//
// Nothing in either stage can fail with the arguments they are given, so this
// stays nil in practice. It exists so a failure is reported rather than
// swallowed.
func (b *Blackhole) Err() error { return b.err }

// Build measures the ring, works out where every character settles, and lays
// out the starfield.
func (b *Blackhole) Build(e *Engine) error {
	if len(b.config.StarColors) == 0 {
		return errBlackholeNoStarColors
	}
	canvas := e.Terminal.Canvas
	// The ring is three tenths of the width or a fifth of the height,
	// whichever is smaller, and never below three.
	b.blackholeRadius = max(min(
		roundHalfEven(float64(canvas.Width)*0.3),
		roundHalfEven(float64(canvas.Height)*0.20),
	), blackholeMinRadius)

	finalGradient, err := NewGradient(b.config.FinalGradientStops, b.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		b.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]

	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)
	b.finalColors = make(map[*Character]Color, len(characters))
	for _, ch := range characters {
		b.finalColors[ch] = mapping.At(ch.InputCoord, fallback)
	}
	for _, ch := range e.Terminal.InputCharacters {
		if ch.UsesInputColors {
			b.preexistingColors = true
			break
		}
	}

	if err := b.prepareBlackhole(e); err != nil {
		return err
	}

	ringSize := len(b.blackholeChars)
	if ringSize == 0 {
		// An input with no characters leaves no ring, and upstream divides by
		// the ring size here. One keeps the arithmetic defined without
		// changing any run that has something to animate.
		ringSize = 1
	}
	b.formationDelay = max(floorDiv(100, ringSize), 6)
	b.frameDelay = b.formationDelay
	b.phase = blackholeForming
	b.awaitingBlackholeChars = append([]*Character(nil), b.blackholeChars...)
	return nil
}

// prepareBlackhole picks the ring, gives every character its star, and points
// everything that is not on the ring at the middle.
func (b *Blackhole) prepareBlackhole(e *Engine) error {
	starSymbols := []string{"*", "'", "`", "¤", "•", "°", "·"}
	starfield, err := NewGradientSteps(
		[]Color{MustParseColor("4a4a4d"), MustParseColor("ffffff")}, blackholeStarfieldSteps, false)
	if err != nil {
		return err
	}
	starfieldColors := starfield.Spectrum
	// One fade to black per starfield shade, so a star dims from the colour it
	// was actually wearing rather than from a shade shared by all of them.
	fadeToBlack := make([]*Gradient, len(starfieldColors))
	for i, color := range starfieldColors {
		fade, err := NewGradientSteps(
			[]Color{color, MustParseColor("000000")}, blackholeFadeSteps, false)
		if err != nil {
			return err
		}
		fadeToBlack[i] = fade
	}

	available := append([]*Character(nil), e.Terminal.InputCharacters...)
	for len(b.blackholeChars) < b.blackholeRadius*blackholeRingChars && len(available) > 0 {
		index := e.Rng.IntBelow(0, len(available))
		b.blackholeChars = append(b.blackholeChars, available[index])
		available = append(available[:index], available[index+1:]...)
	}
	b.onRing = make(map[*Character]bool, len(b.blackholeChars))
	for _, ch := range b.blackholeChars {
		b.onRing[ch] = true
	}

	canvas := e.Terminal.Canvas
	ring := FindCoordsOnCircle(canvas.Center, b.blackholeRadius, len(b.blackholeChars), true)
	for positionIndex, ch := range b.blackholeChars {
		if positionIndex >= len(ring) {
			// FindCoordsOnCircle drops a point that lands on one it has
			// already produced, so the ring can in principle come back
			// shorter than it was asked for. At three points per unit of
			// radius it never does, and upstream indexes it without looking.
			break
		}
		formation, err := ch.Motion.NewPath(blackholeFormationPathID, PathOptions{
			Speed: 0.7, Ease: InOutSine, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := formation.NewWaypoint(ring[positionIndex], nil, ""); err != nil {
			return err
		}

		star := ch.Animation.NewScene(blackholeSceneID, SceneOptions{
			UsesInputColors: ch.UsesInputColors, Frames: 1,
		})
		if err := star.AddFrame("*", 1, VisualParams{Colors: Fg(b.config.BlackholeColor)}); err != nil {
			return err
		}
		// The ring draws over the starfield.
		ch.RegisterEvent(PathActivated, PathCaller(formation.ID), SetLayer(1))

		// The rotation walks the whole ring starting from this character's own
		// slot, so every character travels the same circle a third of a turn
		// apart and the ring reads as one spinning shape.
		rotation, err := ch.Motion.NewPath(blackholeRotationPathID, PathOptions{
			Speed: 0.45, Loop: true,
		})
		if err != nil {
			return err
		}
		for i := 0; i < len(ring); i++ {
			coord := ring[(positionIndex+i)%len(ring)]
			if _, err := rotation.NewWaypoint(coord, nil, strconv.Itoa(len(rotation.Waypoints))); err != nil {
				return err
			}
		}
	}

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		// Every character is on screen from the first frame, as a star. The
		// picture itself is not: the star is drawn somewhere else entirely, in
		// a symbol and a colour of its own.
		e.Terminal.SetCharacterVisibility(ch, true)
		starSymbol := *Choice(e.Rng, starSymbols)
		starColorIndex := e.Rng.IndexBelow(len(starfieldColors))

		starting := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors, Frames: 1,
		})
		if err := starting.AddFrame(starSymbol, 1,
			VisualParams{Colors: Fg(starfieldColors[starColorIndex])}); err != nil {
			return err
		}
		e.ActivateScene(ch, starting.ID)

		if b.onRing[ch] {
			continue
		}

		ch.Motion.SetCoordinate(canvas.RandomCoord(e.Rng, false, false))
		speed := e.Rng.Uniform(0.17, 0.30)
		singularity, err := ch.Motion.NewPath(blackholeSingularityPathID, PathOptions{
			Speed: speed, Ease: InExpo, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := singularity.NewWaypoint(canvas.Center, nil, ""); err != nil {
			return err
		}

		// Synced to distance, so the star dims by how far it has been dragged
		// rather than by how long it has been falling. It is on its last,
		// blank, frame as it reaches the middle.
		fade := fadeToBlack[starColorIndex].Spectrum
		consumed := ch.Animation.NewScene("", SceneOptions{
			Sync: SyncDistance, UsesInputColors: ch.UsesInputColors, Frames: len(fade) + 1,
		})
		for _, color := range fade {
			if err := consumed.AddFrame(starSymbol, 1, VisualParams{Colors: Fg(color)}); err != nil {
				return err
			}
		}
		if err := consumed.AddFrame(" ", 1, VisualParams{}); err != nil {
			return err
		}

		// A falling star passes in front of the ring.
		ch.RegisterEvent(PathActivated, PathCaller(singularity.ID), SetLayer(2))
		ch.RegisterEvent(PathActivated, PathCaller(singularity.ID), ActivateScene(consumed.ID))
		b.awaitingConsumptionChars = append(b.awaitingConsumptionChars, ch)
	}
	Shuffle(e.Rng, b.awaitingConsumptionChars)
	return nil
}

// rotateBlackhole starts the ring turning. The rotation path loops, so it runs
// until something else takes the character over.
func (b *Blackhole) rotateBlackhole(e *Engine) {
	for _, ch := range b.blackholeChars {
		e.ActivatePath(ch, blackholeRotationPathID)
		e.Activate(ch)
	}
}

// collapseBlackhole stretches the ring outwards and then drops it onto the
// middle. The first ring character stays behind as the singularity and runs
// the unstable scene there.
func (b *Blackhole) collapseBlackhole(e *Engine) error {
	canvas := e.Terminal.Canvas
	ring := FindCoordsOnCircle(
		canvas.Center, b.blackholeRadius+blackholeCollapseOvershoot, len(b.blackholeChars), true)
	unstableSymbols := []string{"◦", "◎", "◉", "●", "◉", "◎", "◦"}
	pointMade := false

	for i, ch := range b.blackholeChars {
		if i >= len(ring) {
			break
		}
		expand, err := ch.Motion.NewPath("", PathOptions{Speed: 0.2, Ease: InExpo, HasEase: true})
		if err != nil {
			return err
		}
		if _, err := expand.NewWaypoint(ring[i], nil, ""); err != nil {
			return err
		}
		collapse, err := ch.Motion.NewPath("", PathOptions{Speed: 0.3, Ease: InExpo, HasEase: true})
		if err != nil {
			return err
		}
		if _, err := collapse.NewWaypoint(canvas.Center, nil, ""); err != nil {
			return err
		}
		ch.RegisterEvent(PathComplete, PathCaller(expand.ID), ActivatePath(collapse.ID))

		if !pointMade {
			point := ch.Animation.NewScene("", SceneOptions{
				UsesInputColors: ch.UsesInputColors,
				Frames:          blackholeUnstableCycles * len(unstableSymbols),
			})
			for cycle := 0; cycle < blackholeUnstableCycles; cycle++ {
				for _, symbol := range unstableSymbols {
					color := *Choice(e.Rng, b.config.StarColors)
					if err := point.AddFrame(symbol, 3, VisualParams{Colors: Fg(color)}); err != nil {
						return err
					}
				}
			}
			ch.RegisterEvent(PathComplete, PathCaller(collapse.ID), ActivateScene(point.ID))
			// The singularity draws over everything.
			ch.RegisterEvent(PathComplete, PathCaller(collapse.ID), SetLayer(3))
			pointMade = true
		}

		e.ActivatePath(ch, expand.ID)
		e.Activate(ch)
	}
	return nil
}

// explodeSingularity throws every character out towards its home and lets it
// cool from the explosion colour into the colour it rests at.
func (b *Blackhole) explodeSingularity(e *Engine) error {
	// The explosion has its own fixed palette. It is not StarColors: upstream
	// writes the six colours out again here rather than reading the option,
	// so changing StarColors recolours the unstable singularity and nothing
	// else. ttfx keeps the duplicate literal and so does this.
	explosionColors := []Color{
		MustParseColor("ffcc0d"), MustParseColor("ff7326"), MustParseColor("ff194d"),
		MustParseColor("bf2669"), MustParseColor("702a8c"), MustParseColor("049dbf"),
	}
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		// Thrown to a point near where it belongs, fast, then drawn the rest
		// of the way home slowly. The two speeds are what makes the explosion
		// read as a blast followed by a drift.
		landings := FindCoordsOnCircle(
			ch.InputCoord, blackholeExplosionRadius, blackholeExplosionArc, true)
		nearby, err := ch.Motion.NewPath("", PathOptions{
			Speed: float64(e.Rng.IntBetween(3, 4)) / 10.0, Ease: OutExpo, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := nearby.NewWaypoint(landings[e.Rng.IntBelow(0, len(landings))], nil, ""); err != nil {
			return err
		}
		home, err := ch.Motion.NewPath("", PathOptions{
			Speed: float64(e.Rng.IntBetween(4, 6)) / 100.0, Ease: InCubic, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := home.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
			return err
		}

		explosionColor := *Choice(e.Rng, explosionColors)
		explode := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors, Frames: 1,
		})
		if err := explode.AddFrame(ch.InputSymbol, 1,
			VisualParams{Colors: Fg(explosionColor)}); err != nil {
			return err
		}

		cooling := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors})
		if err := b.buildCoolingScene(cooling, ch, explosionColor, dynamic); err != nil {
			return err
		}

		ch.RegisterEvent(PathComplete, PathCaller(nearby.ID), ActivatePath(home.ID))
		ch.RegisterEvent(PathComplete, PathCaller(nearby.ID), ActivateScene(cooling.ID))
		e.ActivateScene(ch, explode.ID)
		e.ActivatePath(ch, nearby.ID)
		e.Activate(ch)
	}
	return nil
}

// buildCoolingScene fills the ramp a character cools along once it has been
// thrown clear of the explosion.
func (b *Blackhole) buildCoolingScene(
	cooling *Scene, ch *Character, explosionColor Color, dynamic bool,
) error {
	if !dynamic || !b.preexistingColors {
		ramp, err := NewGradientSteps(
			[]Color{explosionColor, b.finalColors[ch]}, blackholeCoolingSteps, false)
		if err != nil {
			return err
		}
		return cooling.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, blackholeCoolingHold, ramp, nil)
	}

	// The input was a picture that was already on the screen, so the character
	// cools back into the colours it arrived with. Both of them: the
	// background ramps as well as the foreground, or a filled panel would come
	// out of the explosion as bare text on nothing.
	input := ch.Animation.InputColors
	if !input.HasFg && !input.HasBg {
		// It arrived with no colours of its own, so it has none to cool into.
		return cooling.AddFrame(ch.InputSymbol, 1, VisualParams{})
	}
	var fgRamp, bgRamp *Gradient
	var err error
	if input.HasFg {
		if fgRamp, err = NewGradientSteps(
			[]Color{explosionColor, input.Fg}, blackholeCoolingSteps, false); err != nil {
			return err
		}
	}
	if input.HasBg {
		if bgRamp, err = NewGradientSteps(
			[]Color{explosionColor, input.Bg}, blackholeCoolingSteps, false); err != nil {
			return err
		}
	}
	return cooling.ApplyGradientToSymbols(
		[]string{ch.InputSymbol}, blackholeCoolingHold, fgRamp, bgRamp)
}

// ringIsStill reports whether every ring character has stopped moving and has
// no scene left to play, which is how the effect knows the singularity has
// finished being unstable.
//
// Upstream asks for "no active path and no active scene". IsActive is the same
// question here: a ring character owns no looping scene, and a scene that runs
// out of frames is retired in the same tick, so the two never disagree.
func (b *Blackhole) ringIsStill() bool {
	for _, ch := range b.blackholeChars {
		if ch.IsActive() {
			return false
		}
	}
	return true
}

// onlyRingIsActive reports whether everything still animating is part of the
// ring, which means the black hole has eaten everything else.
func (b *Blackhole) onlyRingIsActive(e *Engine) bool {
	for _, ch := range e.ActiveCharacters() {
		if !b.onRing[ch] {
			return false
		}
	}
	return true
}

// Advance moves the effect on by one frame and reports whether it is still
// going.
func (b *Blackhole) Advance(e *Engine) bool {
	if b.err != nil {
		return false
	}
	if e.ActiveCount() == 0 && b.phase == blackholeComplete {
		return false
	}

	switch b.phase {
	case blackholeForming:
		switch {
		case len(b.awaitingBlackholeChars) > 0:
			if b.frameDelay > 0 {
				b.frameDelay--
				break
			}
			next := b.awaitingBlackholeChars[0]
			b.awaitingBlackholeChars = b.awaitingBlackholeChars[1:]
			e.ActivatePath(next, blackholeFormationPathID)
			e.ActivateScene(next, blackholeSceneID)
			e.Activate(next)
			b.frameDelay = b.formationDelay
		case e.ActiveCount() == 0:
			b.rotateBlackhole(e)
			b.phase = blackholeConsuming
		}

	case blackholeConsuming:
		if len(b.awaitingConsumptionChars) > 0 {
			for _, ch := range b.awaitingConsumptionChars {
				e.ActivatePath(ch, blackholeSingularityPathID)
				e.Activate(ch)
			}
			b.awaitingConsumptionChars = nil
		} else if b.onlyRingIsActive(e) {
			b.phase = blackholeCollapsing
		}

	case blackholeCollapsing:
		if err := b.collapseBlackhole(e); err != nil {
			b.err = err
			return false
		}
		b.phase = blackholeExploding

	case blackholeExploding:
		if b.ringIsStill() {
			if err := b.explodeSingularity(e); err != nil {
				b.err = err
				return false
			}
			b.phase = blackholeComplete
		}

	case blackholeComplete:
	}

	e.Update()
	return true
}
