package tuiffects

// middleout, ported from ttfx src/effects/middleout.rs, which ports
// TerminalTextEffects effects/effect_middleout.py by ChrisBuilds.
//
// This effect assembles the screen rather than passing over it. Every
// character is teleported onto the canvas centre before it moves anywhere, so
// there is no moment at which the picture is sitting in place waiting to be
// swept. That is why there is no DynamicExistingColors deviation here: showing
// every character at its home cell on the first frame, which is what a sweep
// needs, would give away the whole picture before the effect has drawn a
// single cell of it.

func init() {
	Register(Descriptor{
		Name:        "middleout",
		Description: "The text collapses onto the centre, spreads along one axis, then expands to fill the canvas",
		New:         func() Effect { return NewMiddleout(DefaultMiddleoutConfig()) },
	})
}

// ExpandDirection is the axis middleout spreads along before it expands out to
// the rest of the canvas.
//
// The names carry an Expand prefix because Vertical and Horizontal already
// name the axes of a GradientDirection.
type ExpandDirection int

// The two expansion axes.
const (
	// ExpandVertical spreads the text across the centre row first, then
	// expands up and down.
	ExpandVertical ExpandDirection = iota
	// ExpandHorizontal spreads the text down the centre column first, then
	// expands left and right.
	ExpandHorizontal
)

var expandDirectionNames = map[string]ExpandDirection{
	"vertical":   ExpandVertical,
	"horizontal": ExpandHorizontal,
}

// ParseExpandDirection looks up an expansion axis by its upstream name.
func ParseExpandDirection(name string) (ExpandDirection, bool) {
	d, ok := expandDirectionNames[name]
	return d, ok
}

// MiddleoutConfig tunes the middleout effect.
type MiddleoutConfig struct {
	// StartingColor is what every character wears while it is stacked on the
	// centre and spreading out along the first axis. The closing ramp starts
	// from it.
	StartingColor Color
	// ExpandDirection is the axis the text spreads along first.
	ExpandDirection ExpandDirection
	// CenterMovementSpeed is how fast a character travels during the first
	// expansion, in cells per frame.
	CenterMovementSpeed float64
	// FullMovementSpeed is how fast it travels during the second one.
	FullMovementSpeed float64
	// CenterEasing shapes the first expansion, FullEasing the second.
	CenterEasing Easing
	FullEasing   Easing
	// FinalGradientStops colour the text once it has arrived. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultMiddleoutConfig is upstream's default middleout.
func DefaultMiddleoutConfig() MiddleoutConfig {
	return MiddleoutConfig{
		StartingColor:       MustParseColor("ffffff"),
		ExpandDirection:     ExpandVertical,
		CenterMovementSpeed: 0.6,
		FullMovementSpeed:   0.6,
		CenterEasing:        InOutSine,
		FullEasing:          InOutSine,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// middleoutPhase is which of the two expansions is running.
type middleoutPhase int

const (
	middleoutPhaseCenter middleoutPhase = iota
	middleoutPhaseFull
)

// Middleout collapses the whole input onto the centre of the canvas, spreads
// it out along one axis into a single line, and then expands that line back
// out until every character is home.
type Middleout struct {
	config MiddleoutConfig
	phase  middleoutPhase
}

// NewMiddleout builds the effect.
func NewMiddleout(config MiddleoutConfig) *Middleout {
	return &Middleout{config: config}
}

// Build stacks every character on the centre of the canvas and gives it the
// two paths and the one scene it needs: out to the centre line, then out to
// where it came from, ramping from the starting colour to its final one as it
// goes.
func (m *Middleout) Build(e *Engine) error {
	finalGradient, err := NewGradient(m.config.FinalGradientStops, m.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		m.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		// ttfx keeps a character-to-colour map here because the Python it
		// ports keeps one. Nothing reads it after the character is built, so
		// this is a local.
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			final = ch.Animation.InputColors
		}

		// The teleport comes before the paths are made, because activating a
		// path splices in a segment from wherever the character is standing.
		ch.Motion.SetCoordinate(canvas.Center)

		column, row := ch.InputCoord.Column, canvas.CenterRow
		if m.config.ExpandDirection == ExpandHorizontal {
			column, row = canvas.CenterColumn, ch.InputCoord.Row
		}
		centerPath, err := ch.Motion.NewPath("", PathOptions{
			Speed:   m.config.CenterMovementSpeed,
			Ease:    m.config.CenterEasing,
			HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := centerPath.NewWaypoint(C(column, row), nil, ""); err != nil {
			return err
		}

		fullPath, err := ch.Motion.NewPath("full", PathOptions{
			Speed:   m.config.FullMovementSpeed,
			Ease:    m.config.FullEasing,
			HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := fullPath.NewWaypoint(ch.InputCoord, nil, "full"); err != nil {
			return err
		}

		// Eleven frames: ten gradient steps plus the end stop that is
		// appended after the pair.
		scene := ch.Animation.NewScene("full", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          11,
		})
		var fgGradient, bgGradient *Gradient
		if final.HasFg {
			if fgGradient, err = NewGradientSteps(
				[]Color{m.config.StartingColor, final.Fg}, 10, false); err != nil {
				return err
			}
		}
		// A captured cell can carry a background, and that background is not
		// ramped: it is held at the colour the cell arrived with for every
		// frame of the scene.
		//
		// Upstream ramps it from StartingColor, which is white, exactly as it
		// ramps the foreground. Over piped text nothing carries a background
		// so that never fires. Over a captured screen it means the status
		// bar, the panel and the selection bar all open as solid white slabs
		// with their own text invisible on them, for the half second the
		// picture is assembling. The white-to-colour signature of the effect
		// survives on the foreground, which is what it is for. Under the
		// default colour policy final is a foreground only and this never
		// fires.
		if final.HasBg {
			if bgGradient, err = NewGradientSteps(
				[]Color{final.Bg, final.Bg}, 1, false); err != nil {
				return err
			}
		}
		if fgGradient == nil && bgGradient == nil {
			// A character that arrived with no colours of its own under the
			// dynamic policy. It has nothing to ramp towards, so it just
			// appears.
			if err := scene.AddFrame(ch.InputSymbol, 6, VisualParams{}); err != nil {
				return err
			}
		} else if err := scene.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, 6, fgGradient, bgGradient); err != nil {
			return err
		}

		e.ActivatePath(ch, centerPath.ID)
		// The collapse carries the background too, so a bar travels to the
		// centre as a bar rather than appearing out of nowhere when the
		// second phase starts.
		opening := Fg(m.config.StartingColor)
		if final.HasBg {
			opening = FgBg(m.config.StartingColor, final.Bg)
		}
		ch.Animation.SetAppearance(ch.InputSymbol, opening, ch.UsesInputColors)
		e.Terminal.SetCharacterVisibility(ch, true)
		e.Activate(ch)
	}
	return nil
}

// Advance runs one frame. When the first expansion has finished it starts the
// second one on every character at once, and reports whether anything is still
// moving.
func (m *Middleout) Advance(e *Engine) bool {
	if m.phase == middleoutPhaseCenter && e.ActiveCount() == 0 {
		m.phase = middleoutPhaseFull
		for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
			e.Activate(ch)
		}
		// ttfx iterates the active set here and notes that the canonical
		// order is ascending character id. ActiveCharacters is that order.
		// The slice is copied because the engine reuses it between calls.
		ordered := append([]*Character(nil), e.ActiveCharacters()...)
		for _, ch := range ordered {
			e.ActivatePath(ch, "full")
			e.ActivateScene(ch, "full")
		}
	}
	if e.ActiveCount() == 0 {
		return false
	}
	e.Update()
	return true
}
