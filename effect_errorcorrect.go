package tuiffects

// errorcorrect, ported from ttfx src/effects/errorcorrect.rs, which ports
// TerminalTextEffects effects/effect_errorcorrect.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "errorcorrect",
		Description: "Some characters start in each other's places, then swap back one pair at a time",
		New:         func() Effect { return NewErrorCorrect(DefaultErrorCorrectConfig()) },
	})
}

// ErrorCorrectConfig tunes the errorcorrect effect.
type ErrorCorrectConfig struct {
	// ErrorPairs is the share of the characters that start in the wrong
	// place, between 0 and 1. Each pair takes two characters, so 0.1 over a
	// hundred characters makes ten pairs and misplaces twenty of them.
	ErrorPairs float64
	// SwapDelay is how many frames pass between one pair being released and
	// the next.
	SwapDelay int
	// ErrorColor marks a character that is in the wrong place.
	ErrorColor Color
	// CorrectColor marks a character that has just been put right. It is the
	// colour the closing ramp starts from.
	CorrectColor Color
	// MovementSpeed is how fast a character travels back to its own cell.
	MovementSpeed float64
	// FinalGradientStops colour the text once every pair is home. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultErrorCorrectConfig is upstream's default errorcorrect.
func DefaultErrorCorrectConfig() ErrorCorrectConfig {
	return ErrorCorrectConfig{
		ErrorPairs:    0.1,
		SwapDelay:     6,
		ErrorColor:    MustParseColor("e74c3c"),
		CorrectColor:  MustParseColor("45bf55"),
		MovementSpeed: 0.9,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// errorCorrectWipeIn is the block that grows over a character being taken out
// of the wrong cell, and errorCorrectWipeOut the one that shrinks off it once
// it is back where it belongs.
var (
	errorCorrectWipeIn  = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	errorCorrectWipeOut = []string{"▇", "▆", "▅", "▄", "▃", "▂", "▁"}
)

// ErrorCorrect starts with the whole picture on screen and some of it
// transposed. It then works through the misplaced pairs, flashing each one
// red, wiping it into a block, flying the two characters past each other and
// settling them into their own cells.
//
// The picture is there from the first frame in every colour mode, so this is
// an effect that passes over the screen rather than one that assembles it.
// Upstream already shows every character during the build, so it needs none of
// the reveal-from-frame-one handling a sweep like waves does.
type ErrorCorrect struct {
	config ErrorCorrectConfig

	swapped [][2]*Character
	// misplaced is every character that starts in another character's cell,
	// kept so it can be repainted over whatever it is standing on or flying
	// across.
	misplaced   []*Character
	swapDelay   int
	finalColors map[*Character]ColorPair
	// dynamic is the terminal's colour policy, held so the scenes built per
	// character can tell whether the colour they settle on is the input's own.
	dynamic bool
}

// NewErrorCorrect builds the effect.
func NewErrorCorrect(config ErrorCorrectConfig) *ErrorCorrect {
	return &ErrorCorrect{config: config, finalColors: map[*Character]ColorPair{}}
}

// Build shows the whole picture, picks the pairs that will start transposed,
// and gives each of them the path and the scenes that put it back.
func (x *ErrorCorrect) Build(e *Engine) error {
	gradient, err := NewGradient(x.config.FinalGradientStops, x.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		x.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := gradient.Spectrum[0]
	x.dynamic = e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)
	for _, ch := range characters {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if x.dynamic {
			// May be the empty pair. buildSettleScene has a branch for that,
			// and a character with no colour of its own settles as the
			// terminal default, which is how it arrived.
			final = ch.Animation.InputColors
		}
		x.finalColors[ch] = final
	}

	// Every character is on screen from the first frame wearing the colour it
	// will end on. The effect's whole subject is that a few of them are in the
	// wrong cell, which nobody can see on an empty canvas.
	for _, ch := range characters {
		spawn := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors, Frames: 1})
		if err := spawn.AddFrame(ch.InputSymbol, 1, VisualParams{Colors: x.finalColors[ch]}); err != nil {
			return err
		}
		e.ActivateScene(ch, spawn.ID)
		e.Terminal.SetCharacterVisibility(ch, true)
	}

	correcting, err := NewGradientSteps([]Color{x.config.ErrorColor, x.config.CorrectColor}, 10, false)
	if err != nil {
		return err
	}

	pool := append([]*Character(nil), e.Terminal.InputCharacters...)
	pairCount := int(x.config.ErrorPairs * float64(len(characters)))
	for i := 0; i < pairCount; i++ {
		if len(pool) < 2 {
			break
		}
		first := takeCharacter(e, &pool)
		second := takeCharacter(e, &pool)
		if err := x.transpose(first, second); err != nil {
			return err
		}
		if err := x.transpose(second, first); err != nil {
			return err
		}
		x.swapped = append(x.swapped, [2]*Character{first, second})
		for _, ch := range [2]*Character{first, second} {
			if err := x.configureSwappedCharacter(e, ch, correcting); err != nil {
				return err
			}
			x.layGhost(e, ch)
			x.misplaced = append(x.misplaced, ch)
		}
	}
	return nil
}

// layGhost leaves a background-only character in the cell a swapped character
// belongs to, on the layer below the text.
//
// Both members of a pair leave their cells on the same frame and arrive on the
// same frame, so for the length of a flight neither cell holds anything. Over
// piped text that is an invisible space. Over a captured screen it is a hole
// straight through to the terminal's own background in the middle of a tab
// bar, a filled panel or a selection row, and there is at least one somewhere
// on screen for most of the run. There is no second character to leave
// behind, so the fill has to be left behind instead.
//
// The ghost is never seen while anything occupies the cell, because a layer of
// -1 loses to every real character, and it is never activated, so it does not
// hold the effect open. Scoped to DynamicExistingColors, where a background
// exists at all, so the default path is exactly upstream's.
func (x *ErrorCorrect) layGhost(e *Engine, ch *Character) {
	if !x.dynamic || !ch.Animation.InputColors.HasBg {
		return
	}
	ghost := e.Terminal.AddCharacter(" ", ch.InputCoord)
	ghost.Layer = -1
	ghost.Animation.SetAppearance(" ", Bg(ch.Animation.InputColors.Bg), false)
	e.Terminal.SetCharacterVisibility(ghost, true)
}

// takeCharacter pulls one character out of the pool at random, so no character
// can be picked for two pairs.
func takeCharacter(e *Engine, pool *[]*Character) *Character {
	index := e.Rng.IndexBelow(len(*pool))
	ch := (*pool)[index]
	*pool = append((*pool)[:index], (*pool)[index+1:]...)
	return ch
}

// transpose stands ch in other's cell and gives it the path home.
func (x *ErrorCorrect) transpose(ch, other *Character) error {
	ch.Motion.SetCoordinate(other.InputCoord)
	path, err := ch.Motion.NewPath("input_coord", PathOptions{Speed: x.config.MovementSpeed})
	if err != nil {
		return err
	}
	_, err = path.NewWaypoint(ch.InputCoord, nil, "")
	return err
}

// configureSwappedCharacter builds the whole sequence one misplaced character
// runs through: the red flash, the block wipe in, the flight home, the wipe
// out and the settle.
func (x *ErrorCorrect) configureSwappedCharacter(e *Engine, ch *Character, correcting *Gradient) error {
	final := x.finalColors[ch]

	// Both wipes are allocated before either is filled, so their auto-ids come
	// out in the same order they do upstream.
	wipeIn := ch.Animation.NewScene("", SceneOptions{
		UsesInputColors: ch.UsesInputColors, Frames: len(errorCorrectWipeIn),
	})
	wipeOut := ch.Animation.NewScene("", SceneOptions{
		UsesInputColors: ch.UsesInputColors, Frames: len(errorCorrectWipeOut),
	})
	for _, block := range errorCorrectWipeIn {
		if err := wipeIn.AddFrame(block, 3, VisualParams{Colors: x.over(final, x.config.ErrorColor)}); err != nil {
			return err
		}
	}
	settlesOnInput := x.dynamic
	for i, block := range errorCorrectWipeOut {
		colors := x.over(final, x.config.CorrectColor)
		if i == len(errorCorrectWipeOut)-1 && settlesOnInput {
			// Upstream hands the last frame of the wipe the character's final
			// colours when it is resolving to the input's own, so the block
			// shrinks off a cell that is already back to itself.
			colors = final
		}
		if err := wipeOut.AddFrame(block, 3, VisualParams{Colors: colors}); err != nil {
			return err
		}
	}

	initial := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors, Frames: 1})
	if err := initial.AddFrame(
		ch.InputSymbol, 1, VisualParams{Colors: x.over(final, x.config.ErrorColor)}); err != nil {
		return err
	}
	e.ActivateScene(ch, initial.ID)

	// The error scene flickers between a shaded block and the character itself
	// ten times, which is what reads as the mistake being spotted.
	white := MustParseColor("ffffff")
	errorScene := ch.Animation.NewScene("error", SceneOptions{UsesInputColors: ch.UsesInputColors, Frames: 20})
	for i := 0; i < 10; i++ {
		if err := errorScene.AddFrame(
			"▓", 3, VisualParams{Colors: x.over(final, x.config.ErrorColor)}); err != nil {
			return err
		}
		if err := errorScene.AddFrame(
			ch.InputSymbol, 3, VisualParams{Colors: x.over(final, white)}); err != nil {
			return err
		}
	}

	// The flight is a solid block whose colour is tied to how far it has left
	// to travel, so it turns from the error colour to the correct one as it
	// arrives. A full block paints the whole cell, so there is no background
	// left showing to carry through it.
	flight := ch.Animation.NewScene("", SceneOptions{
		Sync: SyncDistance, UsesInputColors: ch.UsesInputColors, Frames: len(correcting.Spectrum),
	})
	if err := flight.ApplyGradientToSymbols([]string{"█"}, 3, correcting, nil); err != nil {
		return err
	}

	settle, err := x.buildSettleScene(ch, final)
	if err != nil {
		return err
	}

	ch.RegisterEvent(SceneComplete, SceneCaller("error"), ActivateScene(wipeIn.ID))
	ch.RegisterEvent(SceneComplete, SceneCaller(wipeIn.ID), ActivateScene(flight.ID))
	ch.RegisterEvent(SceneComplete, SceneCaller(wipeIn.ID), ActivatePath("input_coord"))
	// A character in flight is drawn over whatever it passes, and drops back
	// to the base layer once it is home.
	ch.RegisterEvent(PathActivated, PathCaller("input_coord"), SetLayer(1))
	ch.RegisterEvent(PathComplete, PathCaller("input_coord"), SetLayer(0))
	ch.RegisterEvent(PathComplete, PathCaller("input_coord"), ActivateScene(wipeOut.ID))
	ch.RegisterEvent(SceneComplete, SceneCaller(wipeOut.ID), ActivateScene(settle.ID))
	return nil
}

// buildSettleScene ramps the character from the correct colour back to the
// colour it ends on.
func (x *ErrorCorrect) buildSettleScene(ch *Character, final ColorPair) (*Scene, error) {
	scene := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors, Frames: 12})
	if !final.HasFg {
		colors := ColorPair{}
		if final.HasBg {
			colors = Bg(final.Bg)
		}
		return scene, scene.AddFrame(ch.InputSymbol, 3, VisualParams{Colors: colors})
	}
	ramp, err := NewGradientSteps([]Color{x.config.CorrectColor, final.Fg}, 10, false)
	if err != nil {
		return nil, err
	}
	for _, color := range ramp.Spectrum {
		if err := scene.AddFrame(ch.InputSymbol, 3, VisualParams{Colors: x.over(final, color)}); err != nil {
			return nil, err
		}
	}
	return scene, nil
}

// over puts one of the effect's own colours on the character while keeping the
// background it arrived with.
//
// Upstream sets a foreground and nothing else, which is right for piped text
// and wrong over a captured screen: a character sitting in a selection bar or
// a filled panel lost its fill for the length of the correction and got it
// back afterwards. A background is only ever present here under
// DynamicExistingColors, so the default behaviour is exactly upstream's.
//
// The settle ramp moves the foreground alone for the same reason. Upstream
// ramps the background from the correct colour too, which would flush a blue
// bar green before it came back to itself.
func (x *ErrorCorrect) over(final ColorPair, fg Color) ColorPair {
	pair := Fg(fg)
	if final.HasBg {
		pair.Bg, pair.HasBg = final.Bg, true
	}
	return pair
}

// Advance releases one pair every SwapDelay frames and reports whether the
// effect is still going.
func (x *ErrorCorrect) Advance(e *Engine) bool {
	switch {
	case len(x.swapped) > 0 && x.swapDelay == 0:
		pair := x.swapped[0]
		x.swapped = x.swapped[1:]
		for _, ch := range pair {
			e.ActivateScene(ch, "error")
			e.Activate(ch)
		}
		x.swapDelay = x.config.SwapDelay
	case x.swapDelay != 0:
		x.swapDelay--
	}
	if e.ActiveCount() > 0 {
		e.Update()
		x.carryMisplacedOverBackgrounds(e)
		return true
	}
	return false
}

// carryMisplacedOverBackgrounds gives a character away from its own cell the
// background of the cell it is currently over.
//
// A swapped character wears its own background, which is right in its own
// cell and wrong everywhere else: standing in, or flying over, a filled bar it
// replaces that bar's fill with nothing for as long as it is there. It covers
// the cell on purpose, so it has to carry what it is covering. Scoped to
// DynamicExistingColors by overInputBackground, so the default path is
// exactly upstream's.
func (x *ErrorCorrect) carryMisplacedOverBackgrounds(e *Engine) {
	if !x.dynamic {
		return
	}
	for _, ch := range x.misplaced {
		if !ch.IsVisible || ch.Motion.CurrentCoord == ch.InputCoord {
			continue
		}
		visual := ch.Animation.CurrentVisual()
		if !visual.Colors.HasFg {
			continue
		}
		ch.Animation.SetAppearance(
			visual.Symbol,
			overInputBackground(e, ch.Motion.CurrentCoord, visual.Colors.Fg),
			ch.UsesInputColors)
	}
}
