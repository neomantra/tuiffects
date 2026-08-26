package tuiffects

// highlight, ported from ttfx src/effects/highlight.rs, which ports
// TerminalTextEffects effects/effect_highlight.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "highlight",
		Description: "A band of brighter colour sweeps across the text, like light moving over it",
		New:         func() Effect { return NewHighlight(DefaultHighlightConfig()) },
	})
}

// HighlightConfig tunes the highlight effect.
type HighlightConfig struct {
	// HighlightBrightness is how much brighter the middle of the band is than
	// the colour the character settles at. It scales lightness, so a value
	// above 1 brightens and a value below 1 darkens.
	HighlightBrightness float64
	// HighlightDirection is the axis the band travels along. The characters
	// are grouped by it and released one group at a time.
	HighlightDirection CharacterGroup
	// HighlightWidth is how many frames the band holds its brightest colour.
	// A wider band spends longer at full brightness, so it reads as a broader
	// stripe of light. It must be at least 1.
	HighlightWidth int
	// FinalGradientStops colour the text the band travels over. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultHighlightConfig is upstream's default highlight.
func DefaultHighlightConfig() HighlightConfig {
	return HighlightConfig{
		HighlightBrightness: 1.75,
		HighlightDirection:  GroupDiagonalBottomLeftToTopRight,
		HighlightWidth:      8,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

const (
	// highlightSceneID is the per-character scene the band runs in. Scene ids
	// are scoped to one character, so every character can use this name.
	highlightSceneID = "highlight"
	// highlightSweepSteps is how many steps the easer takes to release every
	// group. Upstream hard codes it and exposes no knob for it.
	highlightSweepSteps = 100
	// highlightFrameDuration is how long each colour of the band holds.
	highlightFrameDuration = 2
)

// Highlight sweeps a band of brighter colour across the screen. Nothing moves
// and nothing is hidden: every character is on screen from the first frame
// wearing the colour it settles at, and the band brightens each one in turn as
// it passes over.
//
// Upstream drops the map it builds from character to final colour without ever
// reading it, and the Rust port keeps it only to stay faithful. It is left out
// here because a field nothing reads is not faithfulness, it is dead weight.
type Highlight struct {
	config HighlightConfig
	sweep  *highlightSweep
}

// NewHighlight builds the effect.
func NewHighlight(config HighlightConfig) *Highlight {
	return &Highlight{config: config}
}

// Build gives every character the band scene it will run when the sweep
// reaches it, then groups the characters into the bands the sweep travels
// through.
func (h *Highlight) Build(e *Engine) error {
	groups := e.Terminal.GetCharactersGrouped(InputOnly(), h.config.HighlightDirection)
	h.sweep = &highlightSweep{groups: groups, easing: InOutCirc, totalSteps: highlightSweepSteps}

	finalGradient, err := NewGradient(h.config.FinalGradientStops, h.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		h.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		// The colour the character rests at, which is both what it wears
		// before the band arrives and what it returns to after.
		base := Fg(mapping.At(ch.InputCoord, fallback))
		bandFg, hasBandFg := base.Fg, base.HasFg
		if dynamic {
			// The input is a picture that was already on the screen, so the
			// character rests at the colours it arrived with.
			base = ch.Animation.InputColors
			bandFg, hasBandFg = base.Fg, base.HasFg
			if !hasBandFg {
				// It carried no foreground of its own. Upstream would give it
				// no band at all and the light would pass straight over it,
				// which over piped text never happens because every character
				// there is given a gradient colour. Over a captured screen
				// most of the picture is default-coloured shell output, so
				// most of the screen would never light and the effect would
				// read as broken. A neutral grey gives the band something to
				// brighten; the character still rests, and settles, on
				// nothing.
				bandFg, hasBandFg = DynamicNeutralGrey, true
			}
		}

		var spectrum []Color
		if hasBandFg {
			bright := AdjustColorBrightness(bandFg, h.config.HighlightBrightness)
			band, err := NewGradient(
				[]Color{bandFg, bright, bright, bandFg},
				[]int{3, h.config.HighlightWidth, 3}, false)
			if err != nil {
				return err
			}
			spectrum = band.Spectrum
		}

		ch.Animation.SetAppearance(ch.InputSymbol, base, ch.UsesInputColors)
		frames := len(spectrum)
		if frames == 0 {
			frames = 1
		}
		scene := ch.Animation.NewScene(highlightSceneID, SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          frames,
		})
		if len(spectrum) == 0 {
			if err := scene.AddFrame(ch.InputSymbol, highlightFrameDuration,
				VisualParams{Colors: base}); err != nil {
				return err
			}
		}
		for i, color := range spectrum {
			// The background is carried through the band unchanged. Only the
			// foreground brightens, so a filled panel keeps its fill.
			colors := Fg(color)
			if base.HasBg {
				colors = FgBg(color, base.Bg)
			}
			if i == len(spectrum)-1 && !base.HasFg {
				// The band was built on the stand-in grey, so its last frame
				// would leave the character wearing grey. It rests on what it
				// arrived with, which here is nothing.
				colors = base
			}
			if err := scene.AddFrame(ch.InputSymbol, highlightFrameDuration,
				VisualParams{Colors: colors}); err != nil {
				return err
			}
		}

		// Shown from the first frame in every colour mode. The band passes
		// over the screen rather than assembling it, so there is nothing to
		// reveal and upstream already gets this right.
		e.Terminal.SetCharacterVisibility(ch, true)
	}
	return nil
}

// Advance releases the groups the easer reached this step and reports whether
// the effect is still going.
func (h *Highlight) Advance(e *Engine) bool {
	if e.ActiveCount() == 0 && h.sweep.isComplete() {
		return false
	}
	for _, group := range h.sweep.advance() {
		for _, ch := range group {
			e.ActivateScene(ch, highlightSceneID)
			e.Activate(ch)
		}
	}
	e.Update()
	return true
}

// highlightSweep is ttfx's SequenceEaser and EasingTracker, narrowed to the one
// job highlight has for them: walk an eased fraction along the character groups
// and hand back whichever groups that fraction has just reached.
//
// It is local to this file rather than a shared helper because it is the first
// use of the pattern here. If a second effect needs it, promote it then.
type highlightSweep struct {
	groups     [][]*Character
	easing     Easing
	totalSteps int

	currentStep int
	easedValue  float64
}

// isComplete reports whether the sweep has taken all of its steps. Note that
// the last groups are still animating for a while after this turns true, which
// is why Advance also waits on the active count.
func (s *highlightSweep) isComplete() bool { return s.currentStep >= s.totalSteps }

// advance takes one step and returns the groups it released.
//
// Upstream also reports groups an easing curve moved back past, so a bouncing
// curve can take them away again. Highlight ignores that, and its curve is
// InOutCirc, which only ever moves forwards, so the backward case is left out
// rather than written and never run.
func (s *highlightSweep) advance() [][]*Character {
	previous := s.easedValue
	if s.currentStep < s.totalSteps {
		s.currentStep++
		eased := s.easing.Ease(float64(s.currentStep) / float64(s.totalSteps))
		if eased > 1 {
			eased = 1
		}
		if eased < 0 {
			eased = 0
		}
		s.easedValue = eased
	}
	if len(s.groups) == 0 {
		return nil
	}
	// Truncation towards zero, which is upstream's int().
	length := int(s.easedValue * float64(len(s.groups)))
	previousLength := int(previous * float64(len(s.groups)))
	if length <= previousLength {
		return nil
	}
	return s.groups[previousLength:length]
}
