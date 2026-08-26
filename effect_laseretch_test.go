package tuiffects

import (
	"strings"
	"testing"
)

// laserEtchTerminal builds the terminal laseretch needs. The descriptor asks
// for fill characters because the etch order comes from a spanning tree over
// the whole text block, so a test that leaves them out is testing a different
// effect.
func laserEtchTerminal(t *testing.T, input string, width, height int) *Terminal {
	t.Helper()
	descriptor, ok := Lookup("laseretch")
	if !ok {
		t.Fatal("laseretch is not registered")
	}
	if !descriptor.NeedsFillCharacters {
		t.Fatal("the descriptor does not ask for fill characters, so the etch order comes back empty")
	}
	return NewTerminalFromText(input, TerminalConfig{
		Width: width, Height: height, MakeFillCharacters: true,
	})
}

// laserEtchSymbolAt reads the symbol drawn at a canvas coordinate, or "" when
// the cell is empty.
func laserEtchSymbolAt(frame string, canvas *Canvas, coord Coord) string {
	lines := strings.Split(plain(frame), "\n")
	lineIndex := canvas.Top - coord.Row
	if lineIndex < 0 || lineIndex >= len(lines) {
		return ""
	}
	runes := []rune(lines[lineIndex])
	if coord.Column < 1 || coord.Column > len(runes) {
		return ""
	}
	return string(runes[coord.Column-1])
}

// TestLaserEtchEtchesTheInputText runs laseretch to completion and checks it
// hands back what it was given.
//
// Negative control: adding the cooling frames of the spawn scene with a fixed
// symbol, "#", instead of ch.InputSymbol leaves every character resting on
// "#" and the final frame reads as a row of them. Run and watched fail.
func TestLaserEtchEtchesTheInputText(t *testing.T) {
	const input = "laser etch"
	term := laserEtchTerminal(t, input, 22, 4)
	engine := NewEngine(term, NewRng(3))

	frames, err := Run(NewLaserEtch(DefaultLaserEtchConfig()), engine, 20000)
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
	if len(rows) != 1 || rows[0] != input {
		t.Errorf("the final frame reads %q, want %q", rows, input)
	}
	if first := nonBlank(frames[0]); len(first) == 1 && first[0] == input {
		t.Error("the first frame already reads as the finished text")
	}
	if frames[len(frames)/2] == frames[len(frames)-1] {
		t.Error("the middle frame is identical to the last one, so nothing is animating")
	}
}

// TestLaserEtchAimsTheBeamAtEveryCharacterItEtches checks the thing the effect
// is named for. A beam runs up and to the right from its head, one cell per
// row, and its head rests on the character being etched at the moment that
// character appears. The beam is switched off once there is nothing left.
//
// Negative control: walking the beam down and to the left in reposition, row-1
// and column-1 per cell, keeps the head on target but breaks the diagonal, and
// the shape check fails. Run and watched fail.
//
// Negative control: dropping the reposition call from Advance leaves the beam
// parked at the origin, and the head check fails. Run and watched fail.
//
// The beam is switched off in the same Advance that etches the last character,
// so the head is drawn for every character but that one.
func TestLaserEtchAimsTheBeamAtEveryCharacterItEtches(t *testing.T) {
	term := laserEtchTerminal(t, "laser\netch", 14, 4)
	engine := NewEngine(term, NewRng(3))
	effect := NewLaserEtch(DefaultLaserEtchConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(effect.laser.beamChars) != term.Canvas.Top+1 {
		t.Fatalf("the beam has %d cells, want %d, one per canvas row plus the one below it",
			len(effect.laser.beamChars), term.Canvas.Top+1)
	}

	shown := map[*Character]bool{}
	aimed, drawn := 0, 0
	for frame := 1; frame <= 20000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		head := effect.laser.beamChars[0].Motion.CurrentCoord
		for offset, cell := range effect.laser.beamChars {
			want := C(head.Column+offset, head.Row+offset)
			if got := cell.Motion.CurrentCoord; got != want {
				t.Fatalf("on frame %d beam cell %d stands at %v, want %v: the beam is not a "+
					"diagonal running up and to the right of its head", frame, offset, got, want)
			}
		}
		for _, ch := range term.InputCharacters {
			if !ch.IsVisible || shown[ch] {
				continue
			}
			shown[ch] = true
			if head != ch.InputCoord {
				t.Errorf("the character at %v was etched on frame %d with the beam head at %v",
					ch.InputCoord, frame, head)
				continue
			}
			aimed++
			// The beam is switched off in the same Advance that etches the
			// last character, so its head is only on screen for the ones
			// before it.
			if !effect.laser.beamChars[0].IsVisible {
				continue
			}
			drawn++
			if got := laserEtchSymbolAt(engine.Frame(), term.Canvas, head); got != "*" {
				t.Errorf("the cell under the beam head at %v reads %q on frame %d, want the head %q",
					head, got, frame, "*")
			}
		}
	}
	if aimed != len(term.InputCharacters) {
		t.Errorf("the beam was aimed at %d of the %d input characters", aimed, len(term.InputCharacters))
	}
	if drawn != len(term.InputCharacters)-1 {
		t.Errorf("the beam head was drawn on %d cells, want %d, one for every character but the last",
			drawn, len(term.InputCharacters)-1)
	}
	for _, cell := range effect.laser.beamChars {
		if cell.IsVisible {
			t.Error("a beam cell is still visible after the last character was etched")
			break
		}
	}
}

// TestLaserEtchThrowsSparksThatFallToTheBottom checks the other half of the
// effect. Every stop of the beam emits a spark from the beam's head, the spark
// travels down a curved path to the bottom row of the canvas, and it is
// reclaimed and hidden once it has cooled.
//
// Negative control: passing 0 rather than 1 to emitSparks in reposition emits
// nothing and the spark count check fails. Run and watched fail.
func TestLaserEtchThrowsSparksThatFallToTheBottom(t *testing.T) {
	term := laserEtchTerminal(t, "laser\netch", 14, 6)
	engine := NewEngine(term, NewRng(9))
	effect := NewLaserEtch(DefaultLaserEtchConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	flown := map[*Character]bool{}
	movedDown := 0
	for frame := 1; frame <= 20000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		for _, spark := range effect.laser.sparks.Particles {
			if !spark.IsVisible {
				continue
			}
			flown[spark] = true
			if spark.Motion.CurrentCoord.Row < spark.Motion.PreviousCoord.Row {
				movedDown++
			}
		}
	}
	if len(flown) < len(term.InputCharacters) {
		t.Errorf("%d sparks flew for %d etched characters, want at least one each",
			len(flown), len(term.InputCharacters))
	}
	if movedDown == 0 {
		t.Error("no spark ever moved down a row, so nothing fell")
	}
	for spark := range flown {
		if spark.IsVisible {
			t.Error("a spark is still visible after the effect finished")
			break
		}
	}
	if effect.laser.sparks.AvailableCount() != effect.laser.sparks.Len() {
		t.Errorf("%d of the %d sparks came back to the pool, want all of them",
			effect.laser.sparks.AvailableCount(), effect.laser.sparks.Len())
	}
}

// TestLaserEtchAssemblesRatherThanPassesOver pins which kind of effect this
// is. The laser is what puts a character on the canvas, so under
// DynamicExistingColors every input character must still start hidden, the
// opposite of a sweep like waves that shows the whole screen from frame one.
// Getting it backwards gives a run that resolves correctly and hands over the
// finished picture before the beam has cut a single cell.
//
// The canvas is not empty on frame one, because the beam is already on it.
// What must be absent is the input, so the check is on the input characters.
//
// Negative control: adding the waves-style pre-show to the dynamic branch of
// Build, an Animation.SetAppearance plus SetCharacterVisibility(ch, true),
// shows the input on frame one and this fails. Run and watched fail.
func TestLaserEtchAssemblesRatherThanPassesOver(t *testing.T) {
	const width, height = 6, 2
	grid := make([][]InputCell, height)
	for row := range grid {
		grid[row] = make([]InputCell, width)
		for column := range grid[row] {
			grid[row][column] = InputCell{
				Symbol: string(rune('a' + column)),
				Fg:     RGB(20, 200, 40), HasFg: true,
				Bg: RGB(0, 0, 90), HasBg: true,
			}
		}
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: width, Height: height,
		ExistingColorHandling: DynamicExistingColors,
		MakeFillCharacters:    true,
	})
	engine := NewEngine(term, NewRng(4))
	effect := NewLaserEtch(DefaultLaserEtchConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			t.Fatalf("the character at %v is visible before the first frame", ch.InputCoord)
		}
	}
	if !effect.Advance(engine) {
		t.Fatal("the effect ended on its first frame")
	}
	hidden := 0
	for _, ch := range term.InputCharacters {
		if !ch.IsVisible {
			hidden++
		}
	}
	if hidden != len(term.InputCharacters)-1 {
		t.Errorf("%d of the %d input characters are hidden after frame one, want all but the one "+
			"the laser has just cut", hidden, len(term.InputCharacters))
	}

	last := ""
	for frame := 2; frame <= 20000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		last = engine.Frame()
	}
	rows := nonBlank(last)
	if len(rows) != height {
		t.Fatalf("the final frame has %d rows, want %d", len(rows), height)
	}
	for _, row := range rows {
		if row != "abcdef" {
			t.Errorf("the final frame has a row reading %q, want %q", row, "abcdef")
		}
	}
	if !strings.Contains(last, "48;2;0;0;90") {
		t.Error("the final frame does not carry the input background 0;0;90")
	}
	if !strings.Contains(last, "38;2;20;200;40") {
		t.Error("the final frame does not carry the input foreground 20;200;40")
	}
}

// TestLaserEtchGroupPatternEtchesNothing pins an upstream bug that is
// reproduced on purpose. The grouped etch pattern parses to a CharacterGroup
// and is then tested against the enum's member names, which never match, so
// nothing is ever queued and the effect emits exactly one frame. ttfx checked
// that against the reference build and kept it; so does this.
//
// Negative control: queueing GetCharactersGrouped for EtchGroup makes the
// effect etch the text and run for hundreds of frames, and this fails. Run and
// watched fail.
func TestLaserEtchGroupPatternEtchesNothing(t *testing.T) {
	term := laserEtchTerminal(t, "laser etch", 22, 4)
	engine := NewEngine(term, NewRng(3))
	config := DefaultLaserEtchConfig()
	config.EtchPattern = EtchGroup

	frames, err := Run(NewLaserEtch(config), engine, 20000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("the grouped pattern produced %d frames, want exactly 1", len(frames))
	}
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			t.Fatalf("the character at %v was etched, but the grouped branch is dead upstream",
				ch.InputCoord)
		}
	}
}
