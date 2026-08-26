package tuiffects

import (
	"strings"
	"testing"
)

// binaryPathDigitPositions returns the canvas coordinates of every "0" and "1" in a
// rendered frame. The input text of these tests never contains either, so
// anything found is a travelling digit.
func binaryPathDigitPositions(frame string, canvas *Canvas) []Coord {
	var out []Coord
	for lineIndex, line := range strings.Split(plain(frame), "\n") {
		for column, r := range []rune(line) {
			if r == '0' || r == '1' {
				out = append(out, C(column+1, canvas.Top-lineIndex))
			}
		}
	}
	return out
}

// TestBinaryPathRebuildsTheInputText runs binarypath to completion.
//
// Negative control: pointing the collapse and brighten scenes at a fixed
// symbol instead of ch.InputSymbol leaves the final frame reading as that
// symbol. Run and watched fail.
func TestBinaryPathRebuildsTheInputText(t *testing.T) {
	const input = "binary path"
	term := NewTerminalFromText(input, TerminalConfig{Width: 24, Height: 6})
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewBinaryPath(DefaultBinaryPathConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 40000 {
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

// TestBinaryPathSendsTheCodePointsDigitAtATime checks the thing the effect is
// named for. Each character is broken into the binary digits of its code
// point, those digits travel the canvas on a path away from where the
// character belongs, and they end up on that character's own cell.
//
// Negative control: replacing the plotted path with a single waypoint at the
// input coordinate leaves every digit sitting on the character's cell, so no
// digit is ever seen elsewhere and the travel check fails. Run and watched
// fail.
func TestBinaryPathSendsTheCodePointsDigitAtATime(t *testing.T) {
	// "A" is 0x41, so its digits are 01000001.
	term := NewTerminalFromText("A", TerminalConfig{Width: 20, Height: 9})
	engine := NewEngine(term, NewRng(11))
	effect := NewBinaryPath(DefaultBinaryPathConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(term.InputCharacters) != 1 {
		t.Fatalf("the input made %d characters, want 1", len(term.InputCharacters))
	}
	source := term.InputCharacters[0]
	if len(term.AddedCharacters) != 8 {
		t.Fatalf("binarypath added %d digits, want 8", len(term.AddedCharacters))
	}
	var spelled strings.Builder
	for _, digit := range term.AddedCharacters {
		spelled.WriteString(digit.InputSymbol)
	}
	if spelled.String() != "01000001" {
		t.Errorf("the digits spell %q, want %q", spelled.String(), "01000001")
	}

	awayFromSource := 0
	for frame := 1; frame <= 40000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		for _, coord := range binaryPathDigitPositions(engine.Frame(), term.Canvas) {
			if coord != source.InputCoord {
				awayFromSource++
			}
		}
	}
	if awayFromSource == 0 {
		t.Error("no digit was ever drawn away from the character's own cell, so nothing travelled")
	}
	for _, digit := range term.AddedCharacters {
		if digit.Motion.CurrentCoord != source.InputCoord {
			t.Errorf("a digit finished at %v, want the character's cell %v",
				digit.Motion.CurrentCoord, source.InputCoord)
		}
		if digit.IsVisible {
			t.Error("a digit is still visible after the character it carried was rebuilt")
		}
	}
	if !source.IsVisible {
		t.Error("the character was never shown")
	}
}

// TestBinaryPathShowsNoCharacterBeforeItsDigitsArrive pins the ordering the
// effect depends on: a character stays hidden until its own group of digits
// has finished travelling, and the digits of that group are on screen first.
//
// Negative control: showing the source character in Build instead of in
// stepGroups makes the character visible from frame one and this fails. Run
// and watched fail.
func TestBinaryPathShowsNoCharacterBeforeItsDigitsArrive(t *testing.T) {
	term := NewTerminalFromText("Ab", TerminalConfig{Width: 20, Height: 9})
	engine := NewEngine(term, NewRng(5))
	effect := NewBinaryPath(DefaultBinaryPathConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	firstDigit := 0
	firstCharacter := 0
	for frame := 1; frame <= 40000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		if firstDigit == 0 && len(binaryPathDigitPositions(engine.Frame(), term.Canvas)) > 0 {
			firstDigit = frame
		}
		if firstCharacter == 0 {
			for _, ch := range term.InputCharacters {
				if ch.IsVisible {
					firstCharacter = frame
					break
				}
			}
		}
	}
	if firstDigit == 0 {
		t.Fatal("no digit was ever drawn")
	}
	if firstCharacter == 0 {
		t.Fatal("no character was ever shown")
	}
	if firstDigit >= firstCharacter {
		t.Errorf("a character appeared on frame %d and the first digit on frame %d, want the digits first",
			firstCharacter, firstDigit)
	}
}

// TestBinaryPathAssemblesRatherThanPassesOver pins which kind of effect this
// is. Binarypath rebuilds the picture one character at a time out of its
// digits, so under DynamicExistingColors every input character must still
// start hidden. That is the opposite of a sweep like waves, which shows the
// whole screen from frame one and passes over it. Getting it backwards gives a
// run that resolves correctly and shows the finished picture before a single
// digit has landed.
//
// The screen is not empty on frame one, because the digits are already on it.
// What must be absent is the input itself, so the check is on the input
// characters rather than on the frame being blank.
//
// Negative control: adding the waves-style pre-show to Build, an
// Animation.SetAppearance plus SetCharacterVisibility(ch, true) in the dynamic
// branch, shows the input on frame one and this fails. Run and watched fail.
func TestBinaryPathAssemblesRatherThanPassesOver(t *testing.T) {
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
		Width: width, Height: height, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(4))
	effect := NewBinaryPath(DefaultBinaryPathConfig())
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
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			t.Errorf("the character at %v is visible on frame one, want it hidden until its digits arrive",
				ch.InputCoord)
		}
	}
	first := nonBlank(engine.Frame())
	for _, row := range first {
		if strings.ContainsAny(row, "abcdef") {
			t.Errorf("frame one reads %q, want digits only", row)
		}
	}

	last := ""
	for frame := 2; frame <= 40000; frame++ {
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
}

// TestBinaryPathKeepsInputBackgroundsUnderDynamicColors checks that the
// background of a captured cell survives the run. A screen is full of
// selection bars and filled panels, and an effect that sets only a foreground
// blanks all of them for as long as it runs.
//
// Negative control: changing the dynamic branch of Build to Fg(...) of the
// input foreground alone drops the background escape and this fails. Run and
// watched fail.
func TestBinaryPathKeepsInputBackgroundsUnderDynamicColors(t *testing.T) {
	grid := [][]InputCell{{
		{Symbol: "x", Fg: RGB(20, 200, 40), HasFg: true, Bg: RGB(0, 0, 90), HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 1, Height: 1, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(4))
	if _, err := Run(NewBinaryPath(DefaultBinaryPathConfig()), engine, 40000); err != nil {
		t.Fatalf("Run: %v", err)
	}
	final := engine.Frame()
	if !strings.Contains(final, "48;2;0;0;90") {
		t.Errorf("the final frame is %q, want it to carry the input background 0;0;90", final)
	}
	if !strings.Contains(final, "38;2;20;200;40") {
		t.Errorf("the final frame is %q, want it to carry the input foreground 20;200;40", final)
	}
}
