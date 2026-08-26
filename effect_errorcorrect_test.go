package tuiffects

import (
	"fmt"
	"strings"
	"testing"
)

const errorCorrectInput = "error correction in progress"

func errorCorrectTerminal() *Terminal {
	return NewTerminalFromText(errorCorrectInput, TerminalConfig{Width: 40, Height: 3})
}

// TestErrorCorrectPutsEveryCharacterBack runs the effect to completion and
// checks the text it leaves behind is the text it was given, with every
// misplaced character back in its own cell.
//
// Negative control: pointing the waypoint at the cell the character already
// stands in, so it corrects itself where it is, leaves the final frame reading
// "error cirrrctoon in peogress" and this fails.
func TestErrorCorrectPutsEveryCharacterBack(t *testing.T) {
	engine := NewEngine(errorCorrectTerminal(), NewRng(7))
	frames, err := Run(NewErrorCorrect(DefaultErrorCorrectConfig()), engine, 40000)
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
	if len(rows) != 1 || rows[0] != errorCorrectInput {
		t.Errorf("the final frame reads %q, want %q", rows, errorCorrectInput)
	}
	for _, ch := range engine.Terminal.InputCharacters {
		if ch.Motion.CurrentCoord != ch.InputCoord {
			t.Errorf("%q ended at %v, want %v", ch.InputSymbol, ch.Motion.CurrentCoord, ch.InputCoord)
		}
	}
}

// TestErrorCorrectStartsWithPairsInEachOthersCells is the test for the thing
// the effect is named for. An error is two characters holding each other's
// place, not one character sitting somewhere random, and the count of them
// comes from ErrorPairs.
//
// Negative control: standing each character in its own cell rather than its
// partner's leaves nothing misplaced and this fails on the count.
func TestErrorCorrectStartsWithPairsInEachOthersCells(t *testing.T) {
	term := errorCorrectTerminal()
	engine := NewEngine(term, NewRng(7))
	config := DefaultErrorCorrectConfig()
	effect := NewErrorCorrect(config)
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	wantPairs := int(config.ErrorPairs * float64(len(term.InputCharacters)))
	if wantPairs < 1 {
		t.Fatalf("the test input is too short to make a pair: %d characters", len(term.InputCharacters))
	}
	if len(effect.swapped) != wantPairs {
		t.Fatalf("built %d pairs, want %d", len(effect.swapped), wantPairs)
	}

	misplaced := map[*Character]bool{}
	for _, ch := range term.InputCharacters {
		if ch.Motion.CurrentCoord != ch.InputCoord {
			misplaced[ch] = true
		}
	}
	if len(misplaced) != 2*wantPairs {
		t.Errorf("%d characters are misplaced, want %d", len(misplaced), 2*wantPairs)
	}
	for _, pair := range effect.swapped {
		first, second := pair[0], pair[1]
		if first == second {
			t.Error("a pair holds the same character twice")
			continue
		}
		if first.Motion.CurrentCoord != second.InputCoord {
			t.Errorf("%q stands at %v, want its partner's cell %v",
				first.InputSymbol, first.Motion.CurrentCoord, second.InputCoord)
		}
		if second.Motion.CurrentCoord != first.InputCoord {
			t.Errorf("%q stands at %v, want its partner's cell %v",
				second.InputSymbol, second.Motion.CurrentCoord, first.InputCoord)
		}
		if !misplaced[first] || !misplaced[second] {
			t.Error("a swapped character is not counted as misplaced")
		}
	}
}

// TestErrorCorrectShowsTheWholePictureFromTheFirstFrame pins which kind of
// effect this is. It passes over a screen that is already there rather than
// assembling one, so every cell is occupied before the first Advance and stays
// occupied throughout. An effect that hid its characters and revealed them
// would still finish with the right text and would look nothing like this one.
//
// Negative control: leaving the characters hidden during the build, which is
// what an assembling effect does, gives an empty first frame and this fails.
func TestErrorCorrectShowsTheWholePictureFromTheFirstFrame(t *testing.T) {
	term := errorCorrectTerminal()
	engine := NewEngine(term, NewRng(7))
	effect := NewErrorCorrect(DefaultErrorCorrectConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, ch := range term.InputCharacters {
		if !ch.IsVisible {
			t.Fatalf("%q is hidden after the build, so the picture is being assembled", ch.InputSymbol)
		}
	}
	want := len(term.InputCharacters)
	if got := occupiedCells(engine); got != want {
		t.Errorf("the first frame fills %d cells, want %d", got, want)
	}
	// Characters cross over each other in flight, so a cell can be shared for
	// a moment. What must never happen is the picture emptying out.
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		if got := occupiedCells(engine); got < want-2*len(effect.swapped)-2 {
			t.Fatalf("frame %d fills only %d cells, want about %d", i, got, want)
		}
	}
	if got := occupiedCells(engine); got != want {
		t.Errorf("the last frame fills %d cells, want %d", got, want)
	}
}

func occupiedCells(e *Engine) int {
	count := 0
	for _, row := range e.FrameRows() {
		for _, visual := range row {
			if visual != nil {
				count++
			}
		}
	}
	return count
}

// TestErrorCorrectMarksACharacterBeforeAndAfterItMoves checks the correction
// is visible and not just bookkeeping. A character sitting in the wrong cell
// is marked in the error colour, and the same character is marked in the
// correct colour once it is home.
//
// Both checks ignore the full block a character wears in flight, because the
// flight gradient runs from the error colour to the correct one and so shows
// both of them whatever the rest of the effect does. Ignoring it is what makes
// the controls below bite; an earlier version of this test searched the whole
// frame, and the control for the first check passed.
//
// Negative controls, both run:
//   - colouring the marker frame, the error flash and the block wipe in with
//     CorrectColor leaves no error mark on any glyph but the flying block, and
//     the first check fails;
//   - colouring the block wipe out with ErrorColor and starting the settling
//     ramp from it does the same to the correct mark, and the second fails.
func TestErrorCorrectMarksACharacterBeforeAndAfterItMoves(t *testing.T) {
	term := errorCorrectTerminal()
	engine := NewEngine(term, NewRng(7))
	config := DefaultErrorCorrectConfig()
	effect := NewErrorCorrect(config)
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	markedWrong, markedRight := false, false
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		for _, line := range engine.FrameRows() {
			for _, visual := range line {
				if visual == nil || visual.Symbol == "█" || !visual.Colors.HasFg {
					continue
				}
				switch visual.Colors.Fg {
				case config.ErrorColor:
					markedWrong = true
				case config.CorrectColor:
					markedRight = true
				}
			}
		}
	}
	if !markedWrong {
		t.Errorf("no character was ever marked in the error colour %s", config.ErrorColor.Hex())
	}
	if !markedRight {
		t.Errorf("no character was ever marked in the correct colour %s", config.CorrectColor.Hex())
	}
}

// TestErrorCorrectKeepsTheBackgroundWhileCorrecting guards the one deliberate
// change from upstream.
//
// Upstream gives the error flash, the block wipes and the settling ramp a
// foreground and nothing else. Over a captured screen that erases whatever
// background the cell carried, so a selection bar or a filled panel loses its
// fill for the length of the correction and gets it back afterwards. Here the
// background is carried through, and the settling ramp moves the foreground
// alone so the bar does not flush green before it settles.
//
// The one exception is a character in flight, which wears a full block that
// paints the whole cell and has no background left showing.
//
// Negative control: returning a bare Fg from ErrorCorrect.over drops the
// background on the flash and wipe frames and this fails.
func TestErrorCorrectKeepsTheBackgroundWhileCorrecting(t *testing.T) {
	fg := RGB(255, 0, 0)
	bg := RGB(0, 0, 255)
	var row []InputCell
	for _, r := range "selection bar here" {
		row = append(row, InputCell{Symbol: string(r), Fg: fg, HasFg: true, Bg: bg, HasBg: true})
	}
	term := NewTerminalFromCells([][]InputCell{row}, TerminalConfig{
		Width: 20, Height: 1, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(3))
	effect := NewErrorCorrect(DefaultErrorCorrectConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(effect.swapped) == 0 {
		t.Fatal("the test input made no pairs, so nothing is corrected")
	}
	flashes := 0
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		for _, line := range engine.FrameRows() {
			for _, visual := range line {
				if visual == nil || visual.Symbol == "█" {
					continue
				}
				if !visual.Colors.HasBg || visual.Colors.Bg != bg {
					t.Fatalf("frame %d shows %q with no background, so the bar lost its fill",
						i, visual.Symbol)
				}
				if visual.Colors.HasFg && visual.Colors.Fg != fg {
					flashes++
				}
			}
		}
	}
	if flashes == 0 {
		t.Error("no cell ever wore a colour of its own, so nothing was marked")
	}
	last := engine.Frame()
	if got := plain(last); strings.TrimSpace(got) != "selection bar here" {
		t.Errorf("the final frame reads %q, want the input back", got)
	}
	if !strings.Contains(last, fmt.Sprintf("\x1b[38;2;%d;%d;%dm", fg.R, fg.G, fg.B)) {
		t.Error("the final frame does not carry the input's own foreground")
	}
}
