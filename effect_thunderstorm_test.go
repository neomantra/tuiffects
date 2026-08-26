package tuiffects

import (
	"strings"
	"testing"
)

// thunderstormTestConfig is the default storm shortened to a second, so a test
// does not sit through twelve seconds of rain. Nothing else is changed.
func thunderstormTestConfig() ThunderstormConfig {
	config := DefaultThunderstormConfig()
	config.StormTime = 1
	return config
}

// TestThunderstormClearsAndLeavesTheTextAsItWas runs the effect to completion
// and checks the sky clears: the text is back, reading as itself and wearing
// the colour the final gradient gives it, and something happened in between.
//
// Negative control: adding the unfade scene's colours in gradient order
// instead of reversed leaves every character on the dim storm colour, and the
// colour half of this fails. Pointing the unfade scene at a fixed symbol
// instead of ch.InputSymbol leaves the final frame reading as that symbol, and
// the text half fails.
func TestThunderstormClearsAndLeavesTheTextAsItWas(t *testing.T) {
	const input = "thunder"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 8, AnchorText: AnchorC})
	engine := NewEngine(term, NewRng(3))

	frames, err := Run(NewThunderstorm(thunderstormTestConfig()), engine, 20000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 20000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	rows := nonBlank(frames[len(frames)-1])
	if len(rows) != 1 || strings.TrimSpace(rows[0]) != input {
		t.Errorf("the final frame reads %q, want %q", rows, input)
	}
	if frames[len(frames)/2] == frames[0] {
		t.Error("the middle frame is identical to the first one, so nothing is animating")
	}

	gradient, err := NewGradient(
		DefaultThunderstormConfig().FinalGradientStops,
		DefaultThunderstormConfig().FinalGradientSteps, false)
	if err != nil {
		t.Fatalf("NewGradient: %v", err)
	}
	canvas := term.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight, Vertical)
	if err != nil {
		t.Fatalf("BuildCoordinateColorMapping: %v", err)
	}
	for _, ch := range term.InputCharacters {
		want := mapping.At(ch.InputCoord, gradient.Spectrum[0])
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasFg || got.Fg != want {
			t.Errorf("%q at %v settled on %v, want the clear-sky colour %v",
				ch.InputSymbol, ch.InputCoord, got, want)
		}
	}
}

// TestThunderstormShowsTheWholeScreenFromTheFirstFrame guards the rule that
// separates an effect which passes over the screen from one which assembles
// it. The storm plays over a picture that is already there, so under
// DynamicExistingColors every character has to be on screen wearing the colour
// it arrived with from the first frame, backgrounds included. An effect that
// hid the text and revealed it would still end on the right picture and still
// terminate, and would look completely wrong on the way there.
//
// Negative control: dropping the SetCharacterVisibility call at the end of
// Build's per-character loop empties the first frame and this fails on the
// first character it looks at.
func TestThunderstormShowsTheWholeScreenFromTheFirstFrame(t *testing.T) {
	fg, bg := RGB(240, 240, 240), RGB(20, 80, 160)
	grid := [][]InputCell{{
		{Symbol: "s", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
		{Symbol: "k", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
		{Symbol: "y", Fg: fg, HasFg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 12, Height: 5, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(11))
	effect := NewThunderstorm(thunderstormTestConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !effect.Advance(engine) {
		t.Fatal("the effect ended on its first frame")
	}

	for _, ch := range term.InputCharacters {
		if !ch.IsVisible {
			t.Errorf("%q is not on screen in the first frame", ch.InputSymbol)
			continue
		}
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasFg || got.Fg != fg {
			t.Errorf("%q opens on %v, want the foreground it arrived with %v", ch.InputSymbol, got, fg)
		}
		if ch.Animation.InputColors.HasBg && (!got.HasBg || got.Bg != bg) {
			t.Errorf("%q opens on %v, want the background it arrived with %v", ch.InputSymbol, got, bg)
		}
	}

	// The background has to survive the whole run as well, or a selection bar
	// blinks out for the length of the storm.
	for i := 0; i < 20000 && effect.Advance(engine); i++ {
		for _, ch := range term.InputCharacters {
			if !ch.Animation.InputColors.HasBg {
				continue
			}
			if got := ch.Animation.CurrentVisual().Colors; !got.HasBg {
				t.Fatalf("%q lost its background on frame %d", ch.InputSymbol, i+2)
			}
		}
	}
	for _, ch := range term.InputCharacters {
		got := ch.Animation.CurrentVisual().Colors
		if !got.HasFg || got.Fg != fg {
			t.Errorf("%q settled on %v, want the foreground it arrived with %v", ch.InputSymbol, got, fg)
		}
	}
}

// TestThunderstormLightningWalksFromTheSkyToTheFloor checks the thing the
// effect is named for. A strike has to be drawn from the top row of the canvas
// down to the bottom one, in the lightning colour, a cell or two a frame
// rather than all at once, and everything on the screen has to flash when it
// lands.
//
// The strike is started by hand because upstream gives a storm a chance of
// eight in a thousand per frame of starting one, which is not something a test
// should sit and wait for.
//
// Negative control: laying the strike out from the canvas centre row instead
// of canvas.Top leaves the top rows with no cell on them and the coverage
// check fails. Showing the whole of pendingStrikeChars on one frame, instead
// of a batch at a time, makes the strike appear whole and the progressive
// check fails.
func TestThunderstormLightningWalksFromTheSkyToTheFloor(t *testing.T) {
	term := NewTerminalFromText("strike", TerminalConfig{Width: 24, Height: 9, AnchorText: AnchorC})
	engine := NewEngine(term, NewRng(17))
	effect := NewThunderstorm(thunderstormTestConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Run the pre-storm dimming out, so the strike lands on the effect's own
	// storm state rather than on a state the test invented.
	frame := 0
	for frame < 20000 && effect.phase != thunderstormStorm && effect.Advance(engine) {
		frame++
	}
	if effect.phase != thunderstormStorm {
		t.Fatal("the text never finished dimming, so the storm never started")
	}

	before := map[*Character]ColorPair{}
	for _, ch := range term.InputCharacters {
		before[ch] = ch.Animation.CurrentVisual().Colors
	}

	effect.strikeInProgress = true
	effect.lightningStrike(engine)
	strike := append([]*Character(nil), effect.pendingStrikeChars...)
	if len(strike) == 0 {
		t.Fatal("the strike has no cells")
	}

	canvas := term.Canvas
	rowsCovered := map[int]bool{}
	for _, cell := range strike {
		rowsCovered[cell.Motion.CurrentCoord.Row] = true
		visual := cell.Animation.CurrentVisual()
		if visual.Symbol != "|" && visual.Symbol != "/" && visual.Symbol != "\\" {
			t.Errorf("a strike cell is drawn as %q, want one of | / \\", visual.Symbol)
		}
		if !visual.Colors.HasFg || visual.Colors.Fg != DefaultThunderstormConfig().LightningColor {
			t.Errorf("a strike cell is drawn in %v, want the lightning colour %v",
				visual.Colors, DefaultThunderstormConfig().LightningColor)
		}
	}
	for row := canvas.Bottom; row <= canvas.Top; row++ {
		if !rowsCovered[row] {
			t.Errorf("no strike cell is on row %d, so the strike does not reach from the sky to the floor", row)
		}
	}

	// Walk the strike onto the screen and watch where it arrives.
	appearedOn := map[*Character]int{}
	step := 0
	for step < 200 && len(effect.pendingStrikeChars) != 0 && effect.Advance(engine) {
		step++
		for _, cell := range strike {
			if cell.IsVisible && appearedOn[cell] == 0 {
				appearedOn[cell] = step
			}
		}
	}
	if len(effect.pendingStrikeChars) != 0 {
		t.Fatal("the strike never finished being drawn")
	}
	for _, cell := range strike {
		if appearedOn[cell] == 0 {
			t.Fatalf("a strike cell at %v never appeared", cell.Motion.CurrentCoord)
		}
	}

	first, last := strike[0], strike[0]
	for _, cell := range strike {
		if appearedOn[cell] < appearedOn[first] {
			first = cell
		}
		if appearedOn[cell] > appearedOn[last] {
			last = cell
		}
	}
	if first.Motion.CurrentCoord.Row != canvas.Top {
		t.Errorf("the strike starts on row %d, want the top of the canvas (%d)",
			first.Motion.CurrentCoord.Row, canvas.Top)
	}
	if appearedOn[last] <= appearedOn[first] {
		t.Error("the whole strike appeared on one frame, so it is not being drawn a batch at a time")
	}

	// The landing flashes everything on the screen.
	flashed := false
	for i := 0; i < 40 && !flashed; i++ {
		for _, ch := range term.InputCharacters {
			if ch.Animation.CurrentVisual().Colors != before[ch] {
				flashed = true
				break
			}
		}
		if !flashed && !effect.Advance(engine) {
			break
		}
	}
	if !flashed {
		t.Error("nothing on the screen changed colour when the strike landed, so there was no flash")
	}
}

// TestThunderstormRainFallsFromAboveTheCanvas checks the other half of the
// storm. A raindrop has to enter from over the top of the canvas and fall,
// which is what puts weather in front of the text.
//
// Negative control: aiming the fall waypoint at the raindrop's own origin
// instead of below the canvas floor fails both halves. A drop with nowhere to
// go completes its path on the frame it is emitted and reclaims itself, so
// none is ever seen on screen and none ever moves down a row.
func TestThunderstormRainFallsFromAboveTheCanvas(t *testing.T) {
	term := NewTerminalFromText("rain", TerminalConfig{Width: 20, Height: 8, AnchorText: AnchorC})
	engine := NewEngine(term, NewRng(5))
	effect := NewThunderstorm(thunderstormTestConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	frame := 0
	for frame < 20000 && effect.phase != thunderstormStorm && effect.Advance(engine) {
		frame++
	}
	if effect.phase != thunderstormStorm {
		t.Fatal("the storm never started")
	}

	// Follow every drop the pool owns and record how far each one fell.
	started := map[*Character]int{}
	fell := 0
	spawnedAbove := false
	for step := 0; step < 400 && effect.Advance(engine); step++ {
		for _, drop := range effect.rain.Particles {
			if !drop.IsVisible {
				delete(started, drop)
				continue
			}
			row := drop.Motion.CurrentCoord.Row
			if _, seen := started[drop]; !seen {
				started[drop] = row
				if row > term.Canvas.Top {
					spawnedAbove = true
				}
				continue
			}
			if row < started[drop] {
				fell++
			}
		}
	}
	if !spawnedAbove {
		t.Error("no raindrop entered from above the top of the canvas")
	}
	if fell == 0 {
		t.Error("no raindrop ever moved down a row, so the rain is not falling")
	}
}

// TestThunderstormBakedEaseMatchesTheEngine pins the one deviation in this
// port. The lightning flash runs on a parameterised bezier curve, which this
// engine's Easing enumeration has no room for, so the curve is baked into the
// scene's frames instead of being handed to the engine.
//
// This checks the baking against the engine itself: a scene baked with one of
// the engine's own curves has to play exactly what the engine plays when it is
// given that curve to ease with, tick for tick and for the same number of
// ticks.
//
// Negative control: truncating instead of rounding half to even in
// thunderstormEaseSchedule, or mapping a tick straight to a colour rather than
// through the frame's duration, makes the two sequences diverge and this
// fails.
func TestThunderstormBakedEaseMatchesTheEngine(t *testing.T) {
	const duration = 6
	gradient, err := NewGradientSteps([]Color{RGB(20, 20, 40), RGB(240, 240, 255)}, 7, true)
	if err != nil {
		t.Fatalf("NewGradientSteps: %v", err)
	}
	colors := make([]ColorPair, len(gradient.Spectrum))
	for i, color := range gradient.Spectrum {
		colors[i] = Fg(color)
	}

	term := NewTerminalFromText("xx", TerminalConfig{Width: 6, Height: 3})
	engine := NewEngine(term, NewRng(1))
	eased, baked := term.InputCharacters[0], term.InputCharacters[1]

	engineScene := eased.Animation.NewScene("flash", SceneOptions{Ease: InCirc, HasEase: true})
	for _, pair := range colors {
		if err := engineScene.AddFrame("x", duration, VisualParams{Colors: pair}); err != nil {
			t.Fatalf("AddFrame: %v", err)
		}
	}
	bakedScene := baked.Animation.NewScene("flash", SceneOptions{})
	schedule := thunderstormEaseSchedule(len(colors), duration, InCirc.Ease)
	if err := thunderstormAddEasedFrames(bakedScene, "x", colors, schedule); err != nil {
		t.Fatalf("thunderstormAddEasedFrames: %v", err)
	}

	engine.ActivateScene(eased, "flash")
	engine.ActivateScene(baked, "flash")
	total := len(colors) * duration
	for tick := 1; tick <= total+10; tick++ {
		engine.StepAnimation(eased)
		engine.StepAnimation(baked)
		want := eased.Animation.CurrentVisual().Colors
		got := baked.Animation.CurrentVisual().Colors
		if got != want {
			t.Fatalf("tick %d: the baked scene shows %v, the eased scene shows %v", tick, got, want)
		}
		if tick == total && !baked.Animation.ActiveSceneIsComplete() {
			t.Errorf("the baked scene is still running after %d ticks, want it finished with the eased one", total)
		}
	}
	if !eased.Animation.ActiveSceneIsComplete() || !baked.Animation.ActiveSceneIsComplete() {
		t.Error("one of the two scenes never finished")
	}
}

// TestThunderstormLightningLeavesTheTextGlowing checks what a strike leaves
// behind. As each cell of a spent strike fades out it lights up the character
// it was standing over, which then cools back to the colour the rest of the
// screen is wearing. A character that is not on screen is left alone.
//
// The cell is put over the character by hand: which cells of a strike land on
// text is a matter of where the strike fell, and this is about what happens
// when one does.
//
// Negative control: dropping the ActivateScene call from makeCharGlow, or
// looking the character up by the strike cell's InputCoord instead of where it
// currently is, leaves the character on its own colour and the glow check
// fails. Dropping the IsVisible guard makes the hidden character glow and the
// last check fails.
func TestThunderstormLightningLeavesTheTextGlowing(t *testing.T) {
	// Anchored in the middle, so the corner a strike character is created in
	// is not itself a text cell.
	term := NewTerminalFromText("glow", TerminalConfig{Width: 12, Height: 5, AnchorText: AnchorC})
	engine := NewEngine(term, NewRng(23))
	config := thunderstormTestConfig()
	effect := NewThunderstorm(config)
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(effect.availableStrikeChars) < 2 {
		t.Fatal("there are no strike characters to stand over the text")
	}

	target := term.InputCharacters[0]
	cell := effect.availableStrikeChars[0]
	cell.Motion.SetCoordinate(target.InputCoord)
	effect.makeCharGlow(engine, cell)

	if len(effect.pendingGlowChars) != 1 || effect.pendingGlowChars[0] != target {
		t.Fatalf("the strike lit up %v, want just %q", effect.pendingGlowChars, target.InputSymbol)
	}
	got := target.Animation.CurrentVisual().Colors
	if !got.HasFg || got.Fg != config.GlowingTextColor {
		t.Errorf("%q is wearing %v after the strike, want the glow colour %v",
			target.InputSymbol, got, config.GlowingTextColor)
	}

	// It cools from there back down to the colour the storm left it on.
	for step := 0; step < 400 && !target.Animation.ActiveSceneIsComplete(); step++ {
		engine.StepAnimation(target)
	}
	cooled := target.Animation.CurrentVisual().Colors
	if cooled == got {
		t.Error("the glow never cooled, so the character is still lit up")
	}

	// A character that is not on screen has nothing to light up.
	hidden := term.InputCharacters[1]
	engine.Terminal.SetCharacterVisibility(hidden, false)
	effect.pendingGlowChars = effect.pendingGlowChars[:0]
	cell.Motion.SetCoordinate(hidden.InputCoord)
	effect.makeCharGlow(engine, cell)
	if len(effect.pendingGlowChars) != 0 {
		t.Error("a character that is not on screen was lit up by the strike")
	}
}

// TestThunderstormEndsOnAnEmptyScreen covers the one deviation in this port
// that is not scoped to a colour policy. A host can hand the engine a screen
// with nothing on it, and there is no storm to run over nothing.
//
// Negative control: dropping the empty guard at the end of Build leaves the
// effect in the waiting phase with no character to report the dimming
// finished, so Advance keeps returning true and this runs to the cap.
func TestThunderstormEndsOnAnEmptyScreen(t *testing.T) {
	term := NewTerminalFromCells(nil, TerminalConfig{Width: 8, Height: 3})
	if len(term.InputCharacters) != 0 {
		t.Fatalf("the terminal has %d characters, want none", len(term.InputCharacters))
	}
	engine := NewEngine(term, NewRng(1))
	effect := NewThunderstorm(thunderstormTestConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	frames := 0
	for frames < 500 && effect.Advance(engine) {
		frames++
	}
	if frames != 0 {
		t.Errorf("the effect ran for %d frames over an empty screen, want none", frames)
	}
}
