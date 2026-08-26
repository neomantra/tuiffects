package tuiffects

// spray, ported from ttfx src/effects/spray.rs, which ports
// TerminalTextEffects effects/effect_spray.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "spray",
		Description: "Characters shoot out of one point on the edge and land in place",
		New:         func() Effect { return NewSpray(DefaultSprayConfig()) },
	})
}

// SprayPosition is the point on the canvas every character is fired from.
type SprayPosition int

// The nine spray origins. They name a compass point on the canvas edge, plus
// the middle. East is first so the zero value is upstream's default.
const (
	SprayEast SprayPosition = iota
	SprayNorth
	SprayNorthEast
	SpraySouth
	SpraySouthEast
	SprayWest
	SprayNorthWest
	SpraySouthWest
	SprayCenter
)

var sprayPositionNames = map[string]SprayPosition{
	"n":      SprayNorth,
	"ne":     SprayNorthEast,
	"e":      SprayEast,
	"se":     SpraySouthEast,
	"s":      SpraySouth,
	"sw":     SpraySouthWest,
	"w":      SprayWest,
	"nw":     SprayNorthWest,
	"center": SprayCenter,
}

// ParseSprayPosition looks up a spray origin by its upstream name.
func ParseSprayPosition(name string) (SprayPosition, bool) {
	p, ok := sprayPositionNames[name]
	return p, ok
}

// SprayConfig tunes the spray effect.
type SprayConfig struct {
	// SprayPosition is where on the canvas the characters are fired from.
	SprayPosition SprayPosition
	// SprayVolume is how many characters leave the nozzle each frame, as a
	// fraction of the total. It is a ceiling: each frame fires somewhere
	// between one character and that many.
	SprayVolume float64
	// MovementSpeedLow and MovementSpeedHigh bound each character's flight
	// speed.
	MovementSpeedLow  float64
	MovementSpeedHigh float64
	// MovementEasing shapes the flight. The default leaves the nozzle fast
	// and drifts into place.
	MovementEasing Easing
	// FinalGradientStops colour the text once it lands. They are ignored when
	// the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultSprayConfig is upstream's default spray.
func DefaultSprayConfig() SprayConfig {
	return SprayConfig{
		SprayPosition:     SprayEast,
		SprayVolume:       0.005,
		MovementSpeedLow:  0.6,
		MovementSpeedHigh: 1.4,
		MovementEasing:    OutExpo,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// Spray fires every character out of a single point and lets it fly to where
// it belongs. Nothing is on the screen until the nozzle reaches it, so the
// text assembles rather than being swept over.
type Spray struct {
	config SprayConfig

	// pending is the characters still in the tank, shuffled, popped from the
	// end.
	pending []*Character
	// volume is the most characters one frame can fire.
	volume int
}

// NewSpray builds the effect.
func NewSpray(config SprayConfig) *Spray {
	return &Spray{config: config, volume: 1}
}

// sprayOrigin is the nozzle for a position on this canvas.
//
// The compass points sit on the canvas edge, and the two that use a half
// measure take it with floor division from the far edge rather than from the
// canvas centre. That is upstream's arithmetic and it puts north and south one
// column left of centre on an odd-width canvas.
func sprayOrigin(canvas *Canvas, position SprayPosition) Coord {
	switch position {
	case SprayCenter:
		return canvas.Center
	case SprayNorth:
		return C(floorDiv(canvas.Right, 2), canvas.Top)
	case SprayNorthWest:
		return C(canvas.Left, canvas.Top)
	case SprayWest:
		return C(canvas.Left, floorDiv(canvas.Top, 2))
	case SpraySouthWest:
		return C(canvas.Left, canvas.Bottom)
	case SpraySouth:
		return C(floorDiv(canvas.Right, 2), canvas.Bottom)
	case SpraySouthEast:
		return C(canvas.Right-1, canvas.Bottom)
	case SprayNorthEast:
		return C(canvas.Right-1, canvas.Top)
	default:
		return C(canvas.Right-1, floorDiv(canvas.Top, 2))
	}
}

// Build parks every character on the nozzle and gives it a path home and a
// scene to wear on the way.
func (s *Spray) Build(e *Engine) error {
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
	origin := sprayOrigin(canvas, s.config.SprayPosition)

	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)
	for _, ch := range characters {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		useInput := dynamic
		if useInput {
			final = ch.Animation.InputColors
		}

		speed := e.Rng.Uniform(s.config.MovementSpeedLow, s.config.MovementSpeedHigh)
		ch.Motion.SetCoordinate(origin)
		path, err := ch.Motion.NewPath("", PathOptions{
			Speed: speed, Ease: s.config.MovementEasing, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := path.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
			return err
		}
		// In flight a character crosses cells that already hold a landed one,
		// so it is lifted a layer for the trip and dropped back on arrival.
		ch.RegisterEvent(PathActivated, PathCaller(path.ID), SetLayer(1))
		ch.RegisterEvent(PathComplete, PathCaller(path.ID), SetLayer(0))

		droplet := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          7,
		})
		if useInput {
			// The character already wears its own colours, so the flight only
			// has to hold them. Upstream ramps here from a random gradient
			// colour, which on a captured screen would flush every cell
			// through the effect's palette before it settled.
			for i := 0; i < 7; i++ {
				if err := droplet.AddFrame(ch.InputSymbol, 20, VisualParams{Colors: final}); err != nil {
					return err
				}
			}
		} else {
			start := *Choice(e.Rng, finalGradient.Spectrum)
			sprayGradient, err := NewGradientSteps([]Color{start, final.Fg}, 7, false)
			if err != nil {
				return err
			}
			if err := droplet.ApplyGradientToSymbols(
				[]string{ch.InputSymbol}, 20, sprayGradient, nil); err != nil {
				return err
			}
		}

		e.ActivateScene(ch, droplet.ID)
		e.ActivatePath(ch, path.ID)
		s.pending = append(s.pending, ch)
	}

	Shuffle(e.Rng, s.pending)
	s.volume = max(int(float64(len(s.pending))*s.config.SprayVolume), 1)
	return nil
}

// Advance fires a burst and reports whether the effect is still going.
func (s *Spray) Advance(e *Engine) bool {
	if len(s.pending) == 0 && e.ActiveCount() == 0 {
		return false
	}
	if len(s.pending) > 0 {
		for i, n := 0, e.Rng.IntBetween(1, s.volume); i < n && len(s.pending) > 0; i++ {
			next := s.pending[len(s.pending)-1]
			s.pending = s.pending[:len(s.pending)-1]
			e.Terminal.SetCharacterVisibility(next, true)
			e.Activate(next)
		}
	}
	e.Update()
	return true
}
