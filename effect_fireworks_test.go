package tuiffects

import (
	"strings"
	"testing"
)

// TestFireworksSettlesIntoTheInputText runs fireworks to completion and checks
// the screen it leaves behind is the one it was given.
//
// Negative control: giving the fall scene a fixed symbol instead of
// ch.InputSymbol leaves the final frame reading as that symbol. Confirmed
// failing.
func TestFireworksSettlesIntoTheInputText(t *testing.T) {
	const input = "fireworks"
	term := NewTerminalFromText(input, TerminalConfig{Width: 20, Height: 8})
	engine := NewEngine(term, NewRng(7))

	frames, err := Run(NewFireworks(DefaultFireworksConfig()), engine, 40000)
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
	// It has to be an animation and not one frame that is already right.
	if frames[0] == frames[len(frames)-1] {
		t.Error("the first frame already equals the last, so nothing animated")
	}
	// The shell symbol only exists while a shell is climbing, so seeing it at
	// all is evidence the launch half of the effect ran.
	if !strings.Contains(plain(strings.Join(frames, "")), DefaultFireworksConfig().FireworkSymbol) {
		t.Error("the firework symbol never appeared, so no shell climbed")
	}
}

// TestFireworksLaunchesShellsThatBurstApart is the test the effect is named
// for.
//
// A firework is a group of characters that climb from one point on the bottom
// edge as a single dot and then scatter. So for every shell: its characters
// all start on the same cell of the bottom row, they all aim at the same
// burst point, that burst point is at or above the row every one of them
// belongs on, and their outward waypoints land inside the burst ellipse and
// are not all the same cell. An effect that gave each character its own origin
// would still resolve to the input text and would look like rain running
// backwards.
//
// Negative control: moving the originX draw out of the shell-boundary branch,
// so every character rolls its own column, makes the shared-start check fail
// on the first shell. A second control, sending every character to the burst
// point itself instead of to a cell in the ellipse around it, fails the
// scatter check. Both confirmed failing.
func TestFireworksLaunchesShellsThatBurstApart(t *testing.T) {
	block := strings.TrimRight(strings.Repeat("abcdefghijklmnopqrstuvwxyz\n", 6), "\n")
	term := NewTerminalFromText(block, TerminalConfig{Width: 26, Height: 16})
	engine := NewEngine(term, NewRng(9))
	effect := NewFireworks(DefaultFireworksConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	canvas := term.Canvas
	if effect.fireworkVolume < 2 {
		t.Fatalf("this input packs %d character per shell, so there is no shell to burst",
			effect.fireworkVolume)
	}

	shells := 0
	for _, shell := range effect.shells {
		if len(shell) == 0 {
			// The first boundary files an empty group. It carries no flight.
			continue
		}
		shells++
		start := shell[0].Motion.CurrentCoord
		if start.Row != canvas.Bottom {
			t.Errorf("a shell starts on row %d, want the bottom row %d", start.Row, canvas.Bottom)
		}
		apex := shell[0].Motion.Path("apex_pth")
		if apex == nil || len(apex.Waypoints) != 1 {
			t.Fatal("a shell has no single-waypoint climb")
		}
		burst := apex.Waypoints[0].Coord
		inEllipse := map[Coord]bool{}
		for _, c := range FindCoordsInCircle(burst, effect.explodeDistance) {
			inEllipse[c] = true
		}

		outward := map[Coord]bool{}
		for _, ch := range shell {
			if got := ch.Motion.CurrentCoord; got != start {
				t.Errorf("%q starts on %v, want the shell's own launch cell %v",
					ch.InputSymbol, got, start)
			}
			climb := ch.Motion.Path("apex_pth")
			if climb == nil || len(climb.Waypoints) != 1 || climb.Waypoints[0].Coord != burst {
				t.Errorf("%q does not climb to the shell's burst point %v", ch.InputSymbol, burst)
				continue
			}
			// Row 1 is the bottom, so a shell that bursts at or above every
			// member's home row has the larger row number.
			if burst.Row < ch.InputCoord.Row {
				t.Errorf("the shell bursts on row %d, below %q's home row %d",
					burst.Row, ch.InputSymbol, ch.InputCoord.Row)
			}
			spread := ch.Motion.Path("explode_pth")
			if spread == nil || len(spread.Waypoints) != 2 {
				t.Errorf("%q has no two-waypoint burst", ch.InputSymbol)
				continue
			}
			out := spread.Waypoints[0].Coord
			if !inEllipse[out] {
				t.Errorf("%q flies to %v, outside the burst ellipse around %v",
					ch.InputSymbol, out, burst)
			}
			outward[out] = true
			if home := ch.Motion.Path("input_pth"); home == nil ||
				len(home.Waypoints) != 1 || home.Waypoints[0].Coord != ch.InputCoord {
				t.Errorf("%q has no path home to %v", ch.InputSymbol, ch.InputCoord)
			}
		}
		if len(outward) < 2 {
			t.Errorf("a shell of %d characters bursts to %d cell, so it does not scatter",
				len(shell), len(outward))
		}
	}
	if shells == 0 {
		t.Fatal("no shell was built")
	}
}

// TestFireworksAssemblesTheScreenRatherThanPassingOver pins which kind of
// effect this is under the colour policy a screen saver runs in.
//
// fireworks assembles: every character is carried in from a cell on the bottom
// edge that is not its own, so it must stay hidden until its shell is
// launched. An effect that passed over the screen would show the whole picture
// from the first frame and animate on top of it. Doing that here would leave
// the shells climbing over the finished screen they are meant to be
// delivering, and the animation would still terminate on the right frame.
//
// So the check is on how many characters are showing rather than on what the
// frame reads: after Build none are, and after one frame only the shell that
// was launched is.
//
// Negative control: calling SetCharacterVisibility(ch, true) for every
// character at the end of Build makes all sixteen show at once and this fails
// on both counts. Confirmed failing.
func TestFireworksAssemblesTheScreenRatherThanPassingOver(t *testing.T) {
	red := RGB(255, 0, 0)
	blue := RGB(0, 0, 255)
	const side = 4
	var grid [][]InputCell
	for row := 0; row < side; row++ {
		var line []InputCell
		for col := 0; col < side; col++ {
			line = append(line, InputCell{
				Symbol: string(rune('a' + row*side + col)),
				Fg:     red, HasFg: true, Bg: blue, HasBg: true,
			})
		}
		grid = append(grid, line)
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: side, Height: side, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(11))
	effect := NewFireworks(DefaultFireworksConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	total := len(term.InputCharacters)
	if total != side*side {
		t.Fatalf("the terminal holds %d characters, want %d", total, side*side)
	}
	if got := visibleCount(term); got != 0 {
		t.Errorf("%d of %d characters are showing before the first frame, want none", got, total)
	}
	if !effect.Advance(engine) {
		t.Fatal("the effect finished on its first frame")
	}
	if got := visibleCount(term); got > effect.fireworkVolume {
		t.Errorf("%d of %d characters are showing after one frame, want at most one shell (%d)",
			got, total, effect.fireworkVolume)
	}

	last := engine.Frame()
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
		last = engine.Frame()
	}
	want := "abcd\nefgh\nijkl\nmnop"
	if got := strings.TrimSpace(plain(last)); got != want {
		t.Fatalf("the final frame reads %q, want %q", got, want)
	}
	if !strings.Contains(last, "\x1b[38;2;255;0;0m") {
		t.Errorf("the final frame does not carry the input's red: %q", last)
	}
	// The fall ramps the background as well as the foreground, so a captured
	// cell gets its background back instead of losing it for the run.
	if !strings.Contains(last, "\x1b[48;2;0;0;255m") {
		t.Errorf("the final frame does not carry the input's blue background: %q", last)
	}
}
