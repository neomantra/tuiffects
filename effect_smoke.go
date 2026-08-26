package tuiffects

// smoke, ported from ttfx src/effects/smoke.rs, which ports
// TerminalTextEffects effects/effect_smoke.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "smoke",
		Description: "Smoke seeps out from one cell across the canvas and colours the text as it passes",
		New:         func() Effect { return NewSmoke(DefaultSmokeConfig()) },
		// The smoke spreads along a spanning tree over the whole canvas, not
		// along the text, so the empty cells must hold characters too.
		NeedsFillCharacters: true,
	})
}

// SmokeConfig tunes the smoke effect.
type SmokeConfig struct {
	// StartingColor is what the text wears before the smoke reaches it. It is
	// ignored when the engine resolves to the input's own colours, except for
	// a character that arrived carrying none.
	StartingColor Color
	// SmokeSymbols are played in order as the smoke passes over a character.
	SmokeSymbols []string
	// SmokeGradientStops colour the smoke itself. They run into the final
	// gradient's stops, reversed, so the smoke thins out into the colour the
	// text is about to take.
	SmokeGradientStops []Color
	// UseWholeCanvas lets the smoke out of the text block and over the whole
	// canvas.
	UseWholeCanvas bool
	// FinalGradientStops colour the text once the smoke has passed. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultSmokeConfig is upstream's default smoke.
func DefaultSmokeConfig() SmokeConfig {
	return SmokeConfig{
		StartingColor:          MustParseColor("7A7A7A"),
		SmokeSymbols:           []string{"░", "▒", "▓", "▒", "░"},
		SmokeGradientStops:     []Color{MustParseColor("242424"), MustParseColor("FFFFFF")},
		UseWholeCanvas:         false,
		FinalGradientStops:     []Color{MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF")},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// smokeDimFactor is how far a character's own foreground is dimmed while it
// waits for the smoke, under DynamicExistingColors only. Half lightness leaves
// the picture readable and plainly dulled, which is the job upstream's flat
// grey starting colour does for piped text. Backgrounds are not dimmed; see
// the comment at the site.
const smokeDimFactor = 0.5

// Smoke lights one cell of the canvas and lets the smoke seep outwards from
// it. Every character sits dull until the smoke arrives, plays a run of block
// symbols while it drifts through, and is left wearing its final colour.
//
// The order the smoke spreads in is a weighted spanning tree: every character
// is given a random cost once, the tree always grows towards the cheapest cell
// on its frontier, and a breadth-first walk of the finished tree is what the
// effect animates, one layer per frame. That is why the smoke wanders and
// pools rather than expanding as a clean ring.
//
// This effect passes over the screen rather than assembling it, so every
// character is visible from the first frame. That is upstream's own behaviour:
// smoke never hides anything, it only changes what a character is wearing.
type Smoke struct {
	config SmokeConfig

	// fillAlg walks the finished tree, one layer per frame. The layer it
	// reaches is the set of characters the smoke arrives at this frame.
	fillAlg *BreadthFirst
}

// NewSmoke builds the effect.
func NewSmoke(config SmokeConfig) *Smoke {
	return &Smoke{config: config}
}

// Build grows the spanning tree the smoke will follow and gives every
// character its two scenes: the smoke drifting through it, and the paint that
// is left behind.
func (s *Smoke) Build(e *Engine) error {
	// The order here is the order the random draws happen in, and the draws
	// are what the spread looks like. The weighted tree is constructed first,
	// which costs a starting coordinate and one weight per character. Then the
	// breadth-first walk's own starting coordinate is drawn; a coordinate that
	// lands on no character leaves the walk to draw its own, exactly as
	// upstream's `starting_char or ...` does.
	limitToTextBoundary := !s.config.UseWholeCanvas
	genAlg, err := NewPrimsWeighted(e, nil, limitToTextBoundary)
	if err != nil {
		return err
	}
	fillStartCoord := e.Terminal.Canvas.RandomCoord(e.Rng, false, limitToTextBoundary)
	fillAlg, err := NewBreadthFirst(e, e.Terminal.CharacterAtInputCoord(fillStartCoord), limitToTextBoundary)
	if err != nil {
		return err
	}

	gradient, err := NewGradient(s.config.FinalGradientStops, s.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		s.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	// Upstream falls back to black rather than to the first colour of the
	// spectrum, so a character standing outside the mapped rectangle goes dark
	// instead of taking the ramp's first stop.
	black := MustParseColor("000000")

	// The smoke runs from its own stops into the final gradient's stops in
	// reverse, so it thins out into the colour the text is about to take.
	smokeStops := append(append([]Color(nil), s.config.SmokeGradientStops...),
		smokeReversedStops(s.config.FinalGradientStops)...)
	smokeGradient, err := NewGradient(smokeStops, []int{3, 4}, false)
	if err != nil {
		return err
	}

	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	characters := e.Terminal.GetCharacters(e.Rng,
		CharacterFilter{Input: true, InnerFill: true, OuterFill: true},
		SortTopToBottomLeftToRight)
	for _, ch := range characters {
		e.Terminal.SetCharacterVisibility(ch, true)

		// finalColors is what the character is left wearing, and base is what
		// it wears until the smoke reaches it.
		var finalColors, base ColorPair
		if dynamic {
			finalColors = ch.Animation.InputColors
			// Deviation from upstream, scoped to this policy. Upstream starts
			// every character at flat black with no background, which reveals
			// piped text as the smoke uncovers it. On a captured screen the
			// picture is already there: black blanks the whole screen for as
			// long as the smoke takes to cross it, and dropping the background
			// blanks every selection bar and filled panel with it. So the
			// character starts wearing a dimmed copy of its own colours, both
			// channels, and brightens to them as the smoke passes.
			if finalColors.HasFg {
				base.Fg, base.HasFg = AdjustColorBrightness(finalColors.Fg, smokeDimFactor), true
			}
			// The background is carried at full strength rather than dimmed.
			// AdjustColorBrightness scales HSL lightness, and a foreground
			// that is dimmed still has a glyph drawn in it, so it stays
			// legible. A background has nothing drawn in it: dimming a dark
			// panel, 1f2937 to 10141c or 222222 to 111111, sinks it into the
			// terminal's own black and the panel disappears instead of
			// dulling. The contrast the effect needs is on the foreground,
			// which still brightens as the smoke passes.
			if finalColors.HasBg {
				base.Bg, base.HasBg = finalColors.Bg, true
			}
			if !base.HasFg && !base.HasBg {
				// A character that arrived with no colours of its own has
				// nothing to dim, so it starts on the same dull grey the other
				// policies start every character on.
				base = Fg(s.config.StartingColor)
			}
		} else {
			finalColors = Fg(mapping.At(ch.InputCoord, black))
			base = Fg(s.config.StartingColor)
		}

		// The paint left behind once the smoke has gone through.
		var paint *Scene
		if dynamic {
			paint = ch.Animation.NewScene("paint", SceneOptions{
				UsesInputColors: ch.UsesInputColors, Frames: 1,
			})
			if err := paint.AddFrame(ch.InputSymbol, 5, VisualParams{Colors: finalColors}); err != nil {
				return err
			}
		} else {
			paintStops := append(append([]Color(nil), s.config.FinalGradientStops...), finalColors.Fg)
			paintGradient, err := NewGradientSteps(paintStops, 5, false)
			if err != nil {
				return err
			}
			paint = ch.Animation.NewScene("paint", SceneOptions{
				UsesInputColors: ch.UsesInputColors, Frames: len(paintGradient.Spectrum),
			})
			if err := paint.ApplyGradientToSymbols(
				[]string{ch.InputSymbol}, 5, paintGradient, nil); err != nil {
				return err
			}
		}

		// The smoke drifting through the cell.
		var smoke *Scene
		if dynamic {
			smoke = ch.Animation.NewScene("smoke", SceneOptions{
				UsesInputColors: ch.UsesInputColors, Frames: len(s.config.SmokeSymbols),
			})
			// Upstream draws the smoke in the character's own colours under
			// this policy rather than in the smoke gradient, so the cell
			// brightens as the smoke reaches it.
			for _, symbol := range s.config.SmokeSymbols {
				if err := smoke.AddFrame(symbol, 10, VisualParams{Colors: finalColors}); err != nil {
					return err
				}
			}
		} else {
			smoke = ch.Animation.NewScene("smoke", SceneOptions{
				UsesInputColors: ch.UsesInputColors,
				Frames:          max(len(s.config.SmokeSymbols), len(smokeGradient.Spectrum)),
			})
			if err := smoke.ApplyGradientToSymbols(
				s.config.SmokeSymbols, 3, smokeGradient, nil); err != nil {
				return err
			}
		}

		ch.RegisterEvent(SceneComplete, SceneCaller(smoke.ID), ActivateScene(paint.ID))
		ch.Animation.SetAppearance(ch.InputSymbol, base, ch.UsesInputColors)
	}

	// The tree is grown to completion here, after the scenes are built,
	// because the weights were drawn before them and the run must consume the
	// engine's random numbers in that order.
	for !genAlg.Complete {
		genAlg.Step(e)
	}

	// The walk never explores its own starting character, so the smoke is lit
	// there by hand.
	e.ActivateScene(fillAlg.StartingChar, "smoke")
	e.Activate(fillAlg.StartingChar)

	s.fillAlg = fillAlg
	return nil
}

// smokeReversedStops copies a stop list back to front. Upstream writes it as a
// Python slice with a negative stride.
func smokeReversedStops(colors []Color) []Color {
	out := make([]Color, len(colors))
	for i, c := range colors {
		out[len(colors)-1-i] = c
	}
	return out
}

// Advance spreads the smoke by one layer of the tree and reports whether the
// effect is still running. It keeps running after the last layer, until every
// character the smoke touched has finished playing it out.
func (s *Smoke) Advance(e *Engine) bool {
	if s.fillAlg.Complete && e.ActiveCount() == 0 {
		return false
	}
	if !s.fillAlg.Complete {
		s.fillAlg.Step()
		for _, ch := range s.fillAlg.ExploredLastStep {
			e.ActivateScene(ch, "smoke")
			e.Activate(ch)
		}
	}
	e.Update()
	return true
}
