package tuiffects

import (
	"math"
	"strings"
	"testing"
)

// TestBubblesSettlesIntoTheInputText runs bubbles to completion and checks the
// screen it leaves behind is the one it was given.
//
// Negative control: adding a frame carrying a fixed symbol instead of
// ch.InputSymbol to the settle scene leaves the final frame reading as that
// symbol. Confirmed failing.
func TestBubblesSettlesIntoTheInputText(t *testing.T) {
	const input = "bubbles float here"
	term := NewTerminalFromText(input, TerminalConfig{Width: 24, Height: 6})
	engine := NewEngine(term, NewRng(13))

	frames, err := Run(NewBubbles(DefaultBubblesConfig()), engine, 40000)
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
	if frames[0] == frames[len(frames)-1] {
		t.Error("the first frame already equals the last, so nothing animated")
	}
	// The burst is the middle of the effect and it is white.
	burst := false
	for _, frame := range frames {
		if strings.Contains(frame, "\x1b[38;2;255;255;255m*") {
			burst = true
			break
		}
	}
	if !burst {
		t.Error("no white burst symbol appeared, so no bubble was seen to pop")
	}
}

// TestBubblesCarriesARingDownAndBurstsIt is the test the effect is named for.
//
// A bubble is not a group of characters that happen to fall together. It is a
// rigid ring: an invisible anchor drifts down the canvas and every character
// riding the bubble is placed back onto a circle around wherever that anchor
// now stands, every single frame. When the ring reaches its row it bursts, and
// the burst throws each character onto a circle three cells wider than the one
// it was riding.
//
// A port that gave each character its own downward path would resolve
// correctly, would animate, and would never draw a bubble.
//
// So this follows one bubble from its release to its burst and checks all
// three: the ring is exactly the circle the geometry gives for the anchor's
// current position, the anchor only ever moves down, and the burst waypoints
// are on the wider circle.
//
// Negative control: removing the setCharacterCoordinates call from move leaves
// the ring where Build put it while the anchor drifts away beneath it, and
// this fails on the first frame of the drift. Confirmed failing.
func TestBubblesCarriesARingDownAndBurstsIt(t *testing.T) {
	term := NewTerminalFromText("bubbles rise and pop all over", TerminalConfig{Width: 32, Height: 12})
	engine := NewEngine(term, NewRng(4))
	effect := NewBubbles(DefaultBubblesConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(effect.waiting) == 0 {
		t.Fatal("Build made no bubbles")
	}
	tracked := effect.waiting[0]
	if len(tracked.characters) < 5 {
		t.Fatalf("the first bubble carries %d characters, want at least 5", len(tracked.characters))
	}

	var anchorRows []int
	var popCenter Coord
	floated, popped := 0, false
	for frame := 0; frame < 40000 && !popped; frame++ {
		wasAnimating := false
		for _, b := range effect.animating {
			if b == tracked {
				wasAnimating = true
				break
			}
		}
		if !effect.Advance(engine) {
			break
		}
		stillAnimating := false
		for _, b := range effect.animating {
			if b == tracked {
				stillAnimating = true
				break
			}
		}
		switch {
		case stillAnimating:
			floated++
			anchorRows = append(anchorRows, tracked.anchor.Motion.CurrentCoord.Row)
			want := FindCoordsOnCircle(tracked.anchor.Motion.CurrentCoord,
				tracked.radius, len(tracked.characters), false)
			for i, ch := range tracked.characters {
				if got := ch.Motion.CurrentCoord; got != want[i] {
					t.Fatalf("frame %d: %q sits at %v, want %v on the ring around the anchor at %v",
						frame, ch.InputSymbol, got, want[i], tracked.anchor.Motion.CurrentCoord)
				}
			}
			popCenter = tracked.anchor.Motion.CurrentCoord
		case wasAnimating:
			popped = true
		}
	}
	if !popped {
		t.Fatal("the tracked bubble never burst")
	}
	if floated < 5 {
		t.Fatalf("the bubble only floated for %d frames, which is too few to have drifted", floated)
	}
	for i := 1; i < len(anchorRows); i++ {
		if anchorRows[i] > anchorRows[i-1] {
			t.Fatalf("the anchor rose from row %d to row %d; a bubble here only drifts down",
				anchorRows[i-1], anchorRows[i])
		}
	}
	if anchorRows[len(anchorRows)-1] >= anchorRows[0] {
		t.Errorf("the anchor started on row %d and ended on row %d, so it never drifted down",
			anchorRows[0], anchorRows[len(anchorRows)-1])
	}

	burst := FindCoordsOnCircle(popCenter, tracked.radius+3, len(tracked.characters), true)
	for i, ch := range tracked.characters {
		if i >= len(burst) {
			break
		}
		path := ch.Motion.Path("pop_out")
		if path == nil {
			t.Fatalf("%q was not thrown outwards: it has no pop_out path", ch.InputSymbol)
		}
		if len(path.Waypoints) != 1 {
			t.Fatalf("%q has %d burst waypoints, want 1", ch.InputSymbol, len(path.Waypoints))
		}
		if got := path.Waypoints[0].Coord; got != burst[i] {
			t.Errorf("%q bursts to %v, want %v on the wider circle around %v",
				ch.InputSymbol, got, burst[i], popCenter)
		}
		// The wider circle really is wider, measured with the column offset
		// halved back out because a terminal cell is about twice as tall as
		// it is wide.
		if reach := bubblesCellDistance(popCenter, burst[i]); reach <= float64(tracked.radius) {
			t.Errorf("%q bursts %.2f cells out, no further than the ring radius of %d",
				ch.InputSymbol, reach, tracked.radius)
		}
	}
}

// TestBubblesAssemblesTheScreenRatherThanPassingOver pins which kind of effect
// this is under the colour policy a screen saver runs in.
//
// bubbles assembles: every character is carried in on a bubble from ten rows
// above the canvas, and one bubble is released every twenty frames. A
// character must therefore stay hidden until the bubble carrying it is let go.
// An effect that passed over the screen would instead show the whole picture
// from the first frame and animate on top of it. Doing that here would put the
// finished text on the screen while thirty bubbles were still queued above it,
// and the bubbles would have nothing left to deliver.
//
// So the check is on how many characters are showing rather than on what the
// frame reads: none at all when Build finishes, and after the first release
// only the handful riding that one bubble.
//
// Negative control: calling SetCharacterVisibility(ch, true) for every
// character at the end of Build makes all thirty-six show at once and this
// fails on both counts. Confirmed failing.
func TestBubblesAssemblesTheScreenRatherThanPassingOver(t *testing.T) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	const side = 6
	red := RGB(255, 0, 0)
	blue := RGB(0, 0, 255)
	var grid [][]InputCell
	for row := 0; row < side; row++ {
		var line []InputCell
		for col := 0; col < side; col++ {
			line = append(line, InputCell{
				Symbol: string(alphabet[row*side+col]),
				Fg:     red, HasFg: true, Bg: blue, HasBg: true,
			})
		}
		grid = append(grid, line)
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: side, Height: side, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(21))
	effect := NewBubbles(DefaultBubblesConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	total := len(term.InputCharacters)
	if total != side*side {
		t.Fatalf("the terminal holds %d characters, want %d", total, side*side)
	}
	if got := bubblesVisibleCount(term); got != 0 {
		t.Errorf("%d of %d characters are showing before the first frame, want none", got, total)
	}

	// The first bubble is released on the frame the delay counter reaches
	// BubbleDelay, and the second one twenty frames after that.
	release := DefaultBubblesConfig().BubbleDelay + 1
	for i := 0; i < release; i++ {
		if !effect.Advance(engine) {
			t.Fatalf("the effect finished after %d frames, before the first bubble was released", i)
		}
	}
	showing := bubblesVisibleCount(term)
	if showing == 0 {
		t.Errorf("no character is showing after %d frames, so no bubble was released", release)
	}
	// A bubble carries at most twenty characters, so a full screen of
	// thirty-six cannot be showing yet.
	if showing > 20 {
		t.Errorf("%d of %d characters are showing after one release, want at most one bubble's worth (20)",
			showing, total)
	}

	frames := []string{engine.Frame()}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		frames = append(frames, engine.Frame())
	}
	last := frames[len(frames)-1]
	want := "abcdef\nghijkl\nmnopqr\nstuvwx\nyz0123\n456789"
	if got := strings.TrimSpace(plain(last)); got != want {
		t.Fatalf("the final frame reads %q, want %q", got, want)
	}
	if !strings.Contains(last, "\x1b[38;2;255;0;0m") {
		t.Errorf("the final frame does not carry the input's red: %q", last)
	}
	if !strings.Contains(last, "\x1b[48;2;0;0;255m") {
		t.Errorf("the final frame does not carry the input's blue background: %q", last)
	}
}

// bubblesCellDistance measures a distance in cells with the column offset
// halved, which undoes the widening FindCoordsOnCircle applies so a circle
// looks round on a terminal.
func bubblesCellDistance(a, b Coord) float64 {
	dc := float64(b.Column-a.Column) / 2
	dr := float64(b.Row - a.Row)
	return math.Sqrt(dc*dc + dr*dr)
}

// bubblesVisibleCount is how many input characters the terminal is showing.
func bubblesVisibleCount(term *Terminal) int {
	n := 0
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			n++
		}
	}
	return n
}
