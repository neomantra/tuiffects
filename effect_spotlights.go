package tuiffects

// spotlights, ported from ttfx src/effects/spotlights.rs, which ports
// TerminalTextEffects effects/effect_spotlights.py by ChrisBuilds.

import (
	"math"
	"strconv"
)

func init() {
	Register(Descriptor{
		Name:        "spotlights",
		Description: "Beams of light search the screen, meet in the middle, then widen until everything is lit",
		New:         func() Effect { return NewSpotlights(DefaultSpotlightsConfig()) },
	})
}

// SpotlightsConfig tunes the spotlights effect.
type SpotlightsConfig struct {
	// BeamWidthRatio sets how wide a beam is. The beam reaches the smaller of
	// the canvas dimensions divided by this number, so a larger value gives a
	// narrower beam. It must be above zero.
	BeamWidthRatio float64
	// BeamFalloff is how much of the beam's width is soft edge, as a fraction
	// of the whole. At 0.3 the outer three tenths fade off, and a character
	// right on the rim shows at a fifth of its brightness. Zero gives a hard
	// edged beam.
	BeamFalloff float64
	// SearchDuration is how many frames the beams wander for before they head
	// for the middle of the canvas.
	SearchDuration int
	// SearchSpeedMin and SearchSpeedMax bound the speed of each leg of a
	// beam's wandering. Every leg picks its own speed from this range, which
	// is what stops several beams moving in step.
	//
	// ttfx carries this as one range argument. Two fields read better in Go
	// and hold the same two numbers.
	SearchSpeedMin float64
	SearchSpeedMax float64
	// SpotlightCount is how many beams search. They all converge on the same
	// point, and only the first of them stays on to do the widening.
	SpotlightCount int
	// FinalGradientStops colour the text the beams travel over. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultSpotlightsConfig is upstream's default spotlights.
func DefaultSpotlightsConfig() SpotlightsConfig {
	return SpotlightsConfig{
		BeamWidthRatio: 2.0,
		BeamFalloff:    0.3,
		SearchDuration: 550,
		SearchSpeedMin: 0.35,
		SearchSpeedMax: 0.75,
		SpotlightCount: 3,
		FinalGradientStops: []Color{
			MustParseColor("ab48ff"), MustParseColor("e7b2b2"), MustParseColor("fffebd"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

const (
	// spotlightsSymbol is the character a beam is made of. It is never shown:
	// a beam is a position, not something drawn.
	spotlightsSymbol = "O"
	// spotlightsFirstPathID is the first leg of a beam's wander. The legs are
	// numbered from zero, and chaining them makes this the only one the effect
	// has to start by hand.
	spotlightsFirstPathID = "0"
	// spotlightsCenterPathID is the path a beam takes to the middle once the
	// search is over.
	spotlightsCenterPathID = "center"
	// spotlightsSearchLegs is how many extra wander targets a beam gets after
	// its first one, so a beam runs a chain of this many plus one paths.
	spotlightsSearchLegs = 10
	// spotlightsDarkBrightness is how far down the unlit colour sits from the
	// lit one.
	spotlightsDarkBrightness = 0.2
	// spotlightsMinBrightness is the floor on the soft edge, so the rim of a
	// beam is dim rather than black.
	spotlightsMinBrightness = 0.2
	// spotlightsExpandLimitRatio ends the effect once the one remaining beam
	// has grown past the larger canvas dimension divided by this.
	spotlightsExpandLimitRatio = 1.5
	// spotlightsMinimumTravelDivisor sets how far apart two consecutive wander
	// targets have to be, as the canvas width divided by this.
	spotlightsMinimumTravelDivisor = 4
	// spotlightsCenterSpeed is the speed of the run to the middle.
	spotlightsCenterSpeed = 0.5
)

// spotlightsNeutralGray is what a character with no foreground of its own is
// lit with when the engine resolves to the input's colours. Upstream calls it
// DYNAMIC_NEUTRAL_GRAY.
var spotlightsNeutralGray = MustParseColor("808080")

// spotlightsColors is the pair of appearances a character alternates between:
// the one it wears inside a beam and the one it wears outside.
type spotlightsColors struct {
	bright ColorPair
	dark   ColorPair
}

// Spotlights sweeps beams of light over a darkened screen. Nothing moves and
// nothing is hidden: every character is on screen from the first frame wearing
// a dimmed version of the colour it will end on, and a beam passing over it
// brings it up to full brightness and drops it back again.
//
// It is an effect that passes over the screen rather than one that assembles
// it, so under DynamicExistingColors the whole picture, backgrounds included,
// is there from frame one. Upstream already builds it that way.
//
// The beams themselves are characters the effect adds to the terminal and
// never makes visible. They exist to carry a path, and their coordinate is
// what the illumination is measured from.
type Spotlights struct {
	config SpotlightsConfig

	spotlights []*Character
	colors     map[*Character]spotlightsColors

	// illuminated is the set lit by the last frame, so the effect knows which
	// characters to put back into the dark when a beam leaves them.
	illuminated map[*Character]struct{}
	// scratch is the previous frame's set, kept to be cleared and refilled
	// rather than reallocated. A full screen beam covers tens of thousands of
	// coordinates per frame, so this is most of what the run would allocate.
	scratch map[*Character]struct{}

	illuminateRange int
	searchDuration  int
	searching       bool
	expanding       bool
	complete        bool
}

// NewSpotlights builds the effect.
func NewSpotlights(config SpotlightsConfig) *Spotlights {
	return &Spotlights{
		config:          config,
		colors:          map[*Character]spotlightsColors{},
		illuminated:     map[*Character]struct{}{},
		scratch:         map[*Character]struct{}{},
		illuminateRange: 1,
		searching:       true,
	}
}

// Build works out the lit and unlit colour of every character, shows the whole
// screen in its unlit colour, and sends the beams off on their wander.
func (s *Spotlights) Build(e *Engine) error {
	if err := s.makeSpotlights(e, s.config.SpotlightCount); err != nil {
		return err
	}

	finalGradient, err := NewGradient(s.config.FinalGradientStops, s.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		s.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		var bright, dark ColorPair
		switch {
		case dynamic:
			// The input is a picture that was already on the screen, so a
			// character is lit back to the colours it arrived with. A cell
			// that carried a background but no foreground, which on a captured
			// screen is a piece of blank chrome, is lit with a neutral grey so
			// the beam is visible crossing it.
			input := ch.Animation.InputColors
			if input.HasFg || input.HasBg {
				brightFg, hasBrightFg := input.Fg, input.HasFg
				if !hasBrightFg && input.HasBg {
					brightFg, hasBrightFg = spotlightsNeutralGray, true
				}
				bright = ColorPair{Fg: brightFg, HasFg: hasBrightFg, Bg: input.Bg, HasBg: input.HasBg}
				dark = bright
				if dark.HasFg {
					dark.Fg = AdjustColorBrightness(dark.Fg, spotlightsDarkBrightness)
				}
				if dark.HasBg {
					dark.Bg = AdjustColorBrightness(dark.Bg, spotlightsDarkBrightness)
				}
			} else {
				bright = Fg(spotlightsNeutralGray)
				dark = Fg(AdjustColorBrightness(spotlightsNeutralGray, spotlightsDarkBrightness))
			}
		default:
			lit := mapping.At(ch.InputCoord, fallback)
			bright = Fg(lit)
			dark = Fg(AdjustColorBrightness(lit, spotlightsDarkBrightness))
		}
		e.Terminal.SetCharacterVisibility(ch, true)
		s.colors[ch] = spotlightsColors{bright: bright, dark: dark}
		ch.Animation.SetAppearance(ch.InputSymbol, dark, ch.UsesInputColors)
	}

	smallest := min(canvas.Right, canvas.Top)
	beam := math.Min(math.Floor(float64(smallest)/s.config.BeamWidthRatio), float64(smallest))
	s.illuminateRange = max(int(beam), 1)
	s.searchDuration = s.config.SearchDuration
	s.searching = true
	s.expanding = false
	s.complete = false

	for _, spotlight := range s.spotlights {
		e.ActivatePath(spotlight, spotlightsFirstPathID)
		e.Activate(spotlight)
	}
	return nil
}

// Advance lights the characters under the beams, then moves the beams on.
func (s *Spotlights) Advance(e *Engine) bool {
	if s.complete {
		return false
	}
	s.illuminateChars(e, s.illuminateRange)

	if s.searching {
		s.searchDuration--
		if s.searchDuration == 0 {
			for _, spotlight := range s.spotlights {
				e.ActivatePath(spotlight, spotlightsCenterPathID)
			}
			s.searching = false
		}
	}

	moving := false
	for _, spotlight := range s.spotlights {
		if !spotlight.Motion.MovementIsComplete() {
			moving = true
			break
		}
	}
	if !moving {
		// The beams have met. They are all on the same coordinate now, so all
		// but the first are dropped and the survivor widens a step per frame.
		if len(s.spotlights) > 1 {
			s.spotlights = s.spotlights[:1]
		}
		s.expanding = true
		s.illuminateRange++
		canvas := e.Terminal.Canvas
		limit := math.Floor(float64(max(canvas.Right, canvas.Top)) / spotlightsExpandLimitRatio)
		if float64(s.illuminateRange) > limit {
			s.complete = true
			s.settle(e)
		}
	}

	e.Update()
	return true
}

// settle puts every character back to its lit colour on the last frame, but
// only when the engine is resolving to the input's own colours.
//
// This is a deviation from upstream, scoped to that one mode so the default
// behaviour stays exactly upstream's. Upstream stops the moment the beam is
// wider than the canvas, and the beam still carries its soft edge, so the run
// ends on a picture with a dimmed vignette around it and any corner the beam's
// ellipse never reached still in the dark. Upstream is written for text that
// arrived from nothing, where a vignette is just how the piece ends. A screen
// saver has to hand the screen back as it found it.
func (s *Spotlights) settle(e *Engine) {
	if e.Terminal.Config.ExistingColorHandling != DynamicExistingColors {
		return
	}
	for ch, colors := range s.colors {
		final := colors.bright
		if override, ok := s.expandColorOverride(e, ch); ok {
			final = override
		}
		ch.Animation.SetAppearance(ch.InputSymbol, final, ch.UsesInputColors)
	}
}

// illuminateChars finds every character under a beam, lights it according to
// how near the middle of the beam it is, and darkens whatever the beams have
// left behind.
func (s *Spotlights) illuminateChars(e *Engine, radius int) {
	inRange := s.scratch
	clear(inRange)
	for _, spotlight := range s.spotlights {
		for _, coord := range FindCoordsInCircle(spotlight.Motion.CurrentCoord, radius) {
			ch := e.Terminal.CharacterAtInputCoord(coord)
			if ch == nil || !spotlightsIsSpotlightable(ch) {
				continue
			}
			// A character with no entry is one the effect never built colours
			// for, which is a fill character the host asked for. Upstream
			// indexes the map straight and would fault on one.
			if _, known := s.colors[ch]; !known {
				continue
			}
			inRange[ch] = struct{}{}
		}
	}

	for ch := range s.illuminated {
		if _, still := inRange[ch]; still {
			continue
		}
		colors := s.colors[ch].dark
		if override, ok := s.expandColorOverride(e, ch); ok {
			colors = override
		}
		ch.Animation.SetAppearance(ch.InputSymbol, colors, ch.UsesInputColors)
	}

	// Everything nearer the beam's middle than this is at full brightness, and
	// everything past it fades to the rim.
	fullBright := float64(radius) * (1 - s.config.BeamFalloff)
	for ch := range inRange {
		distance := math.Inf(1)
		for _, spotlight := range s.spotlights {
			distance = math.Min(distance,
				FindLengthOfLine(spotlight.Motion.CurrentCoord, ch.InputCoord, true))
		}
		colors := s.colors[ch].bright
		if distance > fullBright {
			brightness := math.Max(
				1-(distance-fullBright)/(float64(radius)*s.config.BeamFalloff),
				spotlightsMinBrightness)
			colors = spotlightsAdjustPair(colors, brightness)
		}
		if override, ok := s.expandColorOverride(e, ch); ok {
			colors = override
		}
		ch.Animation.SetAppearance(ch.InputSymbol, colors, ch.UsesInputColors)
	}

	s.scratch = s.illuminated
	s.illuminated = inRange
}

// expandColorOverride is what a character wears during the widening when the
// engine resolves to the input's own colours. The neutral grey a colourless
// character was lit with must not be left on the screen once the beam covers
// everything, so those characters go back to carrying no colour at all.
func (s *Spotlights) expandColorOverride(e *Engine, ch *Character) (ColorPair, bool) {
	if e.Terminal.Config.ExistingColorHandling != DynamicExistingColors || !s.expanding {
		return ColorPair{}, false
	}
	input := ch.Animation.InputColors
	if !input.HasFg && input.HasBg {
		return Bg(input.Bg), true
	}
	if !input.HasFg && !input.HasBg {
		return ColorPair{}, true
	}
	return ColorPair{}, false
}

// makeSpotlights creates the beams, gives each a chain of wander paths that
// loops, and gives each the path to the middle it takes when the search ends.
func (s *Spotlights) makeSpotlights(e *Engine, count int) error {
	canvas := e.Terminal.Canvas
	minimumDistance := floorDiv(canvas.Right, spotlightsMinimumTravelDivisor)

	for i := 0; i < count; i++ {
		spotlight := e.Terminal.AddCharacter(spotlightsSymbol, canvas.RandomCoord(e.Rng, true, false))
		s.spotlights = append(s.spotlights, spotlight)

		targets := make([]Coord, 0, spotlightsSearchLegs+1)
		last := canvas.RandomCoord(e.Rng, false, false)
		targets = append(targets, last)
		for leg := 0; leg < spotlightsSearchLegs; leg++ {
			last = spotlightsCoordAtMinimumDistance(e, last, minimumDistance)
			targets = append(targets, last)
		}

		paths := make([]string, 0, len(targets))
		for _, target := range targets {
			speed := e.Rng.Uniform(s.config.SearchSpeedMin, s.config.SearchSpeedMax)
			path, err := spotlight.Motion.NewPath(strconv.Itoa(len(paths)), PathOptions{
				Speed: speed, Ease: InOutQuad, HasEase: true,
			})
			if err != nil {
				return err
			}
			control := canvas.RandomCoord(e.Rng, true, false)
			if _, err := path.NewWaypoint(target, []Coord{control}, ""); err != nil {
				return err
			}
			paths = append(paths, path.ID)
		}
		e.ChainPaths(spotlight, paths, true)

		center, err := spotlight.Motion.NewPath(spotlightsCenterPathID, PathOptions{
			Speed: spotlightsCenterSpeed, Ease: InOutSine, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := center.NewWaypoint(canvas.Center, nil, ""); err != nil {
			return err
		}
	}
	return nil
}

// spotlightsCoordAtMinimumDistance draws random coordinates until one is far
// enough from the last, so a beam always has somewhere to travel to.
func spotlightsCoordAtMinimumDistance(e *Engine, origin Coord, minimumDistance int) Coord {
	for {
		coord := e.Terminal.Canvas.RandomCoord(e.Rng, false, false)
		if FindLengthOfLine(origin, coord, false) >= float64(minimumDistance) {
			return coord
		}
	}
}

// spotlightsIsSpotlightable skips the cells there is nothing to light: a blank
// that carried no colour of its own.
func spotlightsIsSpotlightable(ch *Character) bool {
	if ch.InputSymbol != " " {
		return true
	}
	input := ch.Animation.InputColors
	return input.HasFg || input.HasBg
}

// spotlightsAdjustPair dims both channels of a pair, leaving an absent channel
// absent.
func spotlightsAdjustPair(colors ColorPair, brightness float64) ColorPair {
	if colors.HasFg {
		colors.Fg = AdjustColorBrightness(colors.Fg, brightness)
	}
	if colors.HasBg {
		colors.Bg = AdjustColorBrightness(colors.Bg, brightness)
	}
	return colors
}
