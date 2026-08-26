package tuiffects

import (
	"fmt"
	"strings"
	"testing"
)

// swarmBlock is a text block big enough that a swarm holds several characters
// rather than one. Swarm size is a tenth of the character count, so a group of
// more than one needs at least fifteen characters.
func swarmBlock(rows, columns int) string {
	var b strings.Builder
	for r := 0; r < rows; r++ {
		b.WriteString(strings.Repeat(fmt.Sprintf("%d", r%10), columns))
		b.WriteByte('\n')
	}
	return b.String()
}

// TestSwarmSettlesIntoTheInputText runs swarm to completion and checks the
// text is back where it belongs, having been somewhere else on the way.
//
// Negative control: pointing the landing path's waypoint at spawn instead of
// ch.InputCoord leaves every character out at its gathering area, so the final
// frame comes back empty and the final frame check fails.
func TestSwarmSettlesIntoTheInputText(t *testing.T) {
	input := swarmBlock(4, 20)
	term := NewTerminalFromText(input, TerminalConfig{Width: 26, Height: 9})
	engine := NewEngine(term, NewRng(11))

	frames, err := Run(NewSwarm(DefaultSwarmConfig()), engine, 40000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 40000 {
		t.Fatal("the effect never finished within the frame cap")
	}

	want := nonBlank(input)
	final := nonBlank(frames[len(frames)-1])
	if strings.Join(final, "\n") != strings.Join(want, "\n") {
		t.Errorf("the final frame reads %q, want %q", final, want)
	}

	// It has to animate: some frame in the middle is not the input.
	middle := nonBlank(frames[len(frames)/2])
	if strings.Join(middle, "\n") == strings.Join(want, "\n") {
		t.Error("the middle frame already reads as the input, so nothing moved")
	}
}

// TestSwarmLaunchesGroupsFromOneSpawn checks the thing the effect is named
// for. A swarm is a group, so every character of one starts life at the same
// point off the canvas, and the text is split into several such groups.
//
// Negative control: replacing the shared spawn in Build with a per character
// canvas.RandomCoord call gives each character its own start, so the members
// of a swarm no longer share a coordinate and the first check fails.
func TestSwarmLaunchesGroupsFromOneSpawn(t *testing.T) {
	term := NewTerminalFromText(swarmBlock(6, 30), TerminalConfig{Width: 34, Height: 10})
	engine := NewEngine(term, NewRng(11))
	effect := NewSwarm(DefaultSwarmConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(effect.swarms) < 2 {
		t.Fatalf("the text was split into %d swarms, want several", len(effect.swarms))
	}
	spawns := map[Coord]bool{}
	largest := 0
	for i, swarm := range effect.swarms {
		if len(swarm) == 0 {
			t.Fatalf("swarm %d is empty", i)
		}
		spawn := swarm[0].Motion.CurrentCoord
		for _, ch := range swarm {
			if ch.Motion.CurrentCoord != spawn {
				t.Fatalf("swarm %d starts split: %q is at %v, not at the swarm's %v",
					i, ch.InputSymbol, ch.Motion.CurrentCoord, spawn)
			}
		}
		if term.Canvas.CoordIsInCanvas(spawn) {
			t.Errorf("swarm %d spawns at %v, which is inside the canvas", i, spawn)
		}
		spawns[spawn] = true
		largest = max(largest, len(swarm))
	}
	if len(spawns) < 2 {
		t.Error("every swarm spawns at the same point, so there is only one group")
	}
	// A tenth of 180 characters is a swarm of eighteen.
	if want := max(roundHalfEven(float64(len(term.InputCharacters))*0.1), 1); largest != want {
		t.Errorf("the largest swarm holds %d characters, want %d", largest, want)
	}
}

// TestSwarmRevealsOneGroupAtATime checks that a swarm launches on its own. The
// effect assembles the screen rather than passing over it, so the canvas
// starts empty and fills group by group; if every character were visible from
// the first frame the swarms would crawl over a finished picture.
//
// This holds under DynamicExistingColors too, which is the case that separates
// an assembling effect from a sweeping one.
//
// Negative control: calling e.Terminal.SetCharacterVisibility(ch, true) for
// every character at the end of Build makes the early frames hold the whole
// text and both checks fail.
func TestSwarmRevealsOneGroupAtATime(t *testing.T) {
	rows := strings.Split(strings.TrimRight(swarmBlock(6, 30), "\n"), "\n")
	cells := make([][]InputCell, len(rows))
	for r, row := range rows {
		cells[r] = make([]InputCell, len(row))
		for c, symbol := range row {
			cells[r][c] = InputCell{Symbol: string(symbol), Fg: RGB(200, 30, 30), HasFg: true}
		}
	}
	term := NewTerminalFromCells(cells, TerminalConfig{
		Width: 34, Height: 10, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(11))
	effect := NewSwarm(DefaultSwarmConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	total := len(term.InputCharacters)
	swarmSize := len(effect.swarms[len(effect.swarms)-1])
	visible := 0
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			visible++
		}
	}
	if visible != 0 {
		t.Errorf("%d of %d characters are visible before the first frame, want none", visible, total)
	}

	// After the first frame only the first swarm is on the canvas.
	if !effect.Advance(engine) {
		t.Fatal("the effect ended on its first frame")
	}
	visible = 0
	for _, ch := range term.InputCharacters {
		if ch.IsVisible {
			visible++
		}
	}
	if visible != swarmSize {
		t.Errorf("%d of %d characters are visible after one frame, want the first swarm's %d",
			visible, total, swarmSize)
	}
}

// TestSwarmFollowsTheLeadIntoTheNextArea checks the coordination: when the
// first member of a swarm reaches the next gathering area, most of the swarm
// is sent after it in the same frame rather than each member arriving on its
// own schedule.
//
// The two coordination settings are run against each other because the count
// that follows is random. At full coordination the whole swarm goes; at zero
// only the one that got there does.
//
// Negative control: deleting the inner loop in Advance that activates the new
// area on the other members leaves one character on the new path in both runs,
// so the full coordination check fails.
func TestSwarmFollowsTheLeadIntoTheNextArea(t *testing.T) {
	followers := func(coordination float64) (int, int) {
		term := NewTerminalFromText(swarmBlock(6, 30), TerminalConfig{Width: 34, Height: 10})
		engine := NewEngine(term, NewRng(11))
		config := DefaultSwarmConfig()
		config.SwarmCoordination = coordination
		effect := NewSwarm(config)
		if err := effect.Build(engine); err != nil {
			t.Fatalf("Build: %v", err)
		}
		for i := 0; i < 40000; i++ {
			was := effect.activeSwarmArea
			if !effect.Advance(engine) {
				break
			}
			if effect.activeSwarmArea == was {
				continue
			}
			onNewArea := 0
			for _, ch := range effect.currentSwarm {
				if ch.Motion.hasActivePath && ch.Motion.activePath == effect.activeSwarmArea {
					onNewArea++
				}
			}
			return onNewArea, len(effect.currentSwarm)
		}
		t.Fatalf("no swarm reached a later gathering area at coordination %v", coordination)
		return 0, 0
	}

	together, size := followers(1.0)
	if together != size {
		t.Errorf("%d of %d characters moved on together at full coordination, want all of them",
			together, size)
	}
	alone, _ := followers(0.0)
	if alone != 1 {
		t.Errorf("%d characters moved on at zero coordination, want only the one that led", alone)
	}
}

// TestSwarmResolvesToInputColors checks the dynamic colour policy: a character
// that arrived with its own foreground and background lands wearing both again
// rather than the effect's own gradient.
//
// Negative control: forcing dynamic to false in Build, so the final colour
// comes from the gradient and the landing scene ramps only a foreground, makes
// the settled colours the gradient's last stop and both checks fail. Cutting
// only the landing scene branches is not enough of a control: the final colour
// map still holds the input's foreground, so the foreground check still
// passes and only the background one fails.
func TestSwarmResolvesToInputColors(t *testing.T) {
	fg, bg := RGB(12, 200, 90), RGB(40, 40, 120)
	cells := [][]InputCell{{
		{Symbol: "x", Fg: fg, HasFg: true, Bg: bg, HasBg: true},
	}}
	term := NewTerminalFromCells(cells, TerminalConfig{
		Width: 12, Height: 5, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(11))
	effect := NewSwarm(DefaultSwarmConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i := 0; i < 40000; i++ {
		if !effect.Advance(engine) {
			break
		}
	}
	got := term.InputCharacters[0].Animation.CurrentVisual().Colors
	if !got.HasFg || got.Fg != fg {
		t.Errorf("the settled foreground is %v, want the input's own %v", got, fg)
	}
	if !got.HasBg || got.Bg != bg {
		t.Errorf("the settled background is %v, want the input's own %v", got, bg)
	}
}
