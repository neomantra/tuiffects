package tuiffects

// burn, ported from ttfx src/effects/burn.rs, which ports
// TerminalTextEffects effects/effect_burn.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "burn",
		Description: "Fire spreads through the text from a random cell, and each character cools into its colour behind it",
		New:         func() Effect { return NewBurn(DefaultBurnConfig()) },
		// The fire spreads along a spanning tree over the whole text block,
		// not over the input characters alone, so the empty cells inside the
		// block must hold a character too. Without this the tree's random
		// starting coordinate can land on nothing and Build fails.
		NeedsFillCharacters: true,
	})
}

// BurnConfig tunes the burn effect.
type BurnConfig struct {
	// StartingColor is the unburnt paper: every character wears it from the
	// first frame until the fire reaches it.
	StartingColor Color
	// BurnColors are the colours a character passes through while it burns.
	BurnColors []Color
	// SmokeChance is how often a character that has finished burning throws
	// off a smoke particle. Zero means no smoke.
	SmokeChance float64
	// FinalGradientStops colour the text once the fire has passed. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultBurnConfig is upstream's default burn.
func DefaultBurnConfig() BurnConfig {
	return BurnConfig{
		StartingColor: MustParseColor("837373"),
		BurnColors: []Color{
			MustParseColor("ffffff"),
			MustParseColor("fff75d"),
			MustParseColor("fe650d"),
			MustParseColor("8A003C"),
			MustParseColor("510100"),
		},
		SmokeChance:            0.5,
		FinalGradientStops:     []Color{MustParseColor("00c3ff"), MustParseColor("ffff1c")},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// burnCharOrder is the shape a character takes as it burns: a spark, a corner
// of block, a full block, then back down to a speck of ash.
var burnCharOrder = []string{"'", ".", "▖", "▙", "█", "▜", "▀", "▝", "."}

// burnSmokeSymbols are what a smoke particle can look like.
var burnSmokeSymbols = []string{".", ",", "'", "`", "#", "*"}

// burnSmokeStops are the two ends of the smoke's own fade, dark grey to light.
var burnSmokeStops = []Color{MustParseColor("504F4F"), MustParseColor("C7C7C7")}

// Burn sets the text alight. A spanning tree grows outwards from a random cell
// and the fire follows it, two to four characters a frame. A character that
// has finished burning cools into its final colour and may give off a smoke
// particle that drifts up off the top of the canvas.
//
// This effect passes over the screen rather than assembling it. Every
// character is visible and wearing StartingColor from the first frame, in
// every colour mode, because burn's subject is paper that is already there.
// So there is no reveal to defer and no waves-style pre-show to add under
// DynamicExistingColors: the deviation that mode needs here is about
// backgrounds, not about visibility. See Build.
type Burn struct {
	config BurnConfig

	// charLinkOrder is the order the spanning tree reached the characters in,
	// consumed from the front, one small group per frame. It holds fill
	// characters as well as input ones; isBurnable skips them.
	charLinkOrder []*Character

	// smoke recycles the smoke particles. A run emits far more of them than
	// the pool holds, so they go round rather than being made afresh.
	smoke *ParticlePool
}

// NewBurn builds the effect.
func NewBurn(config BurnConfig) *Burn {
	return &Burn{config: config}
}

// hasInputColors reports whether a character arrived carrying colours of its
// own. Upstream's BurnIterator._has_input_colors.
func hasInputColors(ch *Character) bool {
	return ch.Animation.InputColors.HasFg || ch.Animation.InputColors.HasBg
}

// isBurnable reports whether the fire has anything to consume at a cell.
//
// A glyph always burns. An empty cell only burns when the input's own colours
// are in play and the cell carried some, which is how a filled panel on a
// captured screen burns along with the text on it. Upstream's
// BurnIterator._is_burnable.
func (b *Burn) isBurnable(e *Engine, ch *Character) bool {
	if ch.InputSymbol != " " {
		return true
	}
	return e.Terminal.Config.ExistingColorHandling != IgnoreExistingColors && hasInputColors(ch)
}

// burnHeldGradient is a gradient that never moves: one colour repeated once
// per entry of the gradient it will be paired against.
//
// ApplyGradientToSymbols pairs a foreground spectrum against a background one,
// so this is how a scene animates its foreground while holding its background
// still. Matching the lengths keeps the pairing one to one, which leaves the
// foreground sequence exactly as it is without a background.
func burnHeldGradient(color Color, length int) *Gradient {
	spectrum := make([]Color, length)
	for i := range spectrum {
		spectrum[i] = color
	}
	return &Gradient{Spectrum: spectrum}
}

// makeSmokeInitializer returns the setup run once on each particle the pool
// creates: the one scene it uses for its whole life, and the layer that puts
// it in front of the text it is rising off.
//
// Upstream rebuilds the smoke gradient inside this, once per particle. It
// draws no random numbers and depends on nothing per particle, so it is built
// once and captured here instead.
func makeSmokeInitializer(gradient *Gradient) func(e *Engine, ch *Character) {
	return func(e *Engine, ch *Character) {
		scene := ch.Animation.NewScene("smoke", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          len(gradient.Spectrum),
		})
		for _, color := range gradient.Spectrum {
			// AddFrame only rejects a duration below one, and this one is
			// fixed, so it cannot fail. The pool's initializer returns no
			// error to report it through in any case.
			_ = scene.AddFrame(ch.InputSymbol, 10, VisualParams{Colors: Fg(color)})
		}
		ch.Layer = 2
	}
}

// Build lights the tree, stocks the smoke pool, and gives every character the
// two scenes it needs: the fire that crosses it and the cooling that follows.
//
// Deviation, scoped to DynamicExistingColors: a character's background is
// carried through both scenes and never ramped. Upstream sets a foreground
// alone while a character burns, then ramps the background from the last fire
// colour to the input background at the end. On piped text there is no
// background to lose. On a captured screen that blanks every selection bar and
// filled panel for the length of the run and then flushes them dark red on the
// way back, so here the background a cell arrived with simply stays put and
// the fire plays over it. The other two colour modes are untouched.
func (b *Burn) Build(e *Engine) error {
	// Upstream builds the tree first, then the pool, then the scenes. The
	// tree only reads the neighbour table, which nothing here changes, but
	// the order is kept so the random draws happen in upstream's sequence.
	tree, err := NewPrimsSimple(e, nil, true)
	if err != nil {
		return err
	}

	smokeGradient, err := NewGradientSteps(burnSmokeStops, 9, false)
	if err != nil {
		return err
	}
	b.smoke, err = NewParticlePool(burnSmokeSymbols, 2000, Coord{}, makeSmokeInitializer(smokeGradient))
	if err != nil {
		return err
	}
	if err := b.smoke.Preallocate(e, 2000); err != nil {
		return err
	}
	// A particle puts itself back once its fade has played out. Upstream
	// registers this inside every emission, where it accumulates one more
	// handler on the particle each time it flies. The pool is capped at what
	// it was preallocated with, so no particle is ever created later and one
	// registration per particle covers the whole run.
	for _, particle := range b.smoke.Particles {
		b.smoke.ReclaimOnEvent(particle, SceneComplete, SceneCaller("smoke"), true, true)
	}

	finalGradient, err := NewGradient(b.config.FinalGradientStops, b.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		b.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]

	fireGradient, err := NewGradientSteps(b.config.BurnColors, 10, false)
	if err != nil {
		return err
	}
	fireLast := fireGradient.Spectrum[len(fireGradient.Spectrum)-1]

	// The whole tree is grown here rather than one edge a frame, so the order
	// the fire spreads in is settled before the first frame. Complete turns
	// true on the step that finds the edge already empty, so the last turn of
	// this loop links nothing; that is upstream's and it costs one step.
	for !tree.Complete {
		tree.Step(e)
	}
	b.charLinkOrder = tree.CharLinkOrder

	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors
	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		e.Terminal.SetCharacterVisibility(ch, true)
		input := ch.Animation.InputColors

		// Unburnt paper. The character shows this from the first frame until
		// the fire reaches it, which is why burn needs no reveal.
		paper := Fg(b.config.StartingColor)
		if dynamic && input.HasBg {
			paper = FgBg(b.config.StartingColor, input.Bg)
		}
		ch.Animation.SetAppearance(ch.InputSymbol, paper, ch.UsesInputColors)

		// The fire itself: the burn shapes spread across the fire spectrum.
		var burnBg *Gradient
		if dynamic && input.HasBg {
			burnBg = burnHeldGradient(input.Bg, len(fireGradient.Spectrum))
		}
		burning := ch.Animation.NewScene("burn", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          len(fireGradient.Spectrum),
		})
		if err := burning.ApplyGradientToSymbols(burnCharOrder, 4, fireGradient, burnBg); err != nil {
			return err
		}

		// The cooling: the character comes back as itself, ramping out of the
		// last fire colour.
		cooling := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors})
		if err := b.addCoolingFrames(cooling, ch, input, dynamic, fireLast, mapping.At(ch.InputCoord, fallback)); err != nil {
			return err
		}

		ch.RegisterEvent(SceneComplete, SceneCaller(burning.ID), ActivateScene(cooling.ID))
		ch.RegisterEvent(SceneComplete, SceneCaller(burning.ID), Callback(b.onBurnComplete))
	}
	return nil
}

// addCoolingFrames fills the scene a character settles into once the fire has
// left it.
//
// Under the default colour policy that is a ramp from the last fire colour to
// the character's place in the final gradient. Under the dynamic policy it is
// a ramp to the foreground the character arrived with, over the background it
// arrived with, held still. A character that arrived with a background and no
// foreground has nothing to ramp, so it goes straight back to its background;
// one that arrived with neither drops to the terminal's own colours.
func (b *Burn) addCoolingFrames(scene *Scene, ch *Character, input ColorPair, dynamic bool, fireLast, finalColor Color) error {
	if !dynamic {
		gradient, err := NewGradientSteps([]Color{fireLast, finalColor}, 8, false)
		if err != nil {
			return err
		}
		for _, color := range gradient.Spectrum {
			if err := scene.AddFrame(ch.InputSymbol, 4, VisualParams{Colors: Fg(color)}); err != nil {
				return err
			}
		}
		return nil
	}
	switch {
	case input.HasFg:
		gradient, err := NewGradientSteps([]Color{fireLast, input.Fg}, 8, false)
		if err != nil {
			return err
		}
		for _, color := range gradient.Spectrum {
			colors := Fg(color)
			if input.HasBg {
				colors = FgBg(color, input.Bg)
			}
			if err := scene.AddFrame(ch.InputSymbol, 4, VisualParams{Colors: colors}); err != nil {
				return err
			}
		}
		return nil
	case input.HasBg:
		return scene.AddFrame(ch.InputSymbol, 4, VisualParams{Colors: Bg(input.Bg)})
	default:
		return scene.AddFrame(ch.InputSymbol, 4, VisualParams{})
	}
}

// onBurnComplete runs when a character has finished burning.
func (b *Burn) onBurnComplete(e *Engine, ch *Character) {
	b.emitSmoke(e, ch.InputCoord)
}

// emitSmoke sends one smoke particle up from a cell, most of the time.
//
// The particle rises to a column within four of where it started, one row
// above the top of the canvas, so it drifts off the screen rather than piling
// up on the last row. Its fade outlasts the climb; when the fade ends the
// particle reclaims itself and goes back on the free list.
func (b *Burn) emitSmoke(e *Engine, origin Coord) {
	if e.Rng.Float() > b.config.SmokeChance {
		return
	}
	b.smoke.Emit(e, origin, "", true, ParticleReset{}, func(e *Engine, particle *Character) {
		// The reset above cleared the particle's paths, so this flight gets a
		// new one. Its scene is kept and is already back at its first frame:
		// a scene resets itself when it finishes, and finishing is what sent
		// the particle back to the pool.
		path, err := particle.Motion.NewPath("", PathOptions{Speed: 0.5})
		if err != nil {
			return
		}
		target := C(e.Rng.IntBetween(origin.Column-4, origin.Column+4), e.Terminal.Canvas.Top+1)
		if _, err := path.NewWaypoint(target, nil, ""); err != nil {
			return
		}
		e.ActivatePath(particle, path.ID)
		e.ActivateScene(particle, "smoke")
	})
}

// Advance lights the next two to four characters the tree reached and runs one
// frame. The effect is over once the tree's order has run out and the last
// character and the last of the smoke have finished.
func (b *Burn) Advance(e *Engine) bool {
	if len(b.charLinkOrder) == 0 && e.ActiveCount() == 0 {
		return false
	}
	lighting := e.Rng.IntBetween(2, 4)
	for i := 0; i < lighting && len(b.charLinkOrder) > 0; i++ {
		next := b.charLinkOrder[0]
		b.charLinkOrder = b.charLinkOrder[1:]
		if !b.isBurnable(e, next) {
			continue
		}
		e.ActivateScene(next, "burn")
		e.Activate(next)
	}
	e.Update()
	// A smoke particle carries a foreground and no background and sits on the
	// layer above the text, and every particle's rise ends on the top row,
	// which on a captured screen is the title or tab bar. Without this it
	// takes that cell's fill away for as long as it is over it, so the bar
	// flickers with black notches for the whole run.
	carryAddedCharactersOverBackgrounds(e)
	return true
}
