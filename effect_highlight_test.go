package tuiffects

import (
	"strings"
	"testing"
)

// highlightGreyConfig is the default highlight over a single dark grey final
// gradient. A uniform base colour makes "this character is lit" a plain
// comparison, and grey is dark enough that brightening it always changes it;
// the default gradient ends on white, which brightens to itself.
func highlightGreyConfig() HighlightConfig {
	config := DefaultHighlightConfig()
	config.FinalGradientStops = []Color{MustParseColor("404040")}
	return config
}

// highlightRun drives the effect by hand and records, for every character, the
// first frame its foreground left the colour it rests at and the colour it was
// showing when the run ended.
type highlightRun struct {
	base      map[*Character]ColorPair
	firstLit  map[*Character]int
	brightest map[*Character]Color
	final     map[*Character]ColorPair
	frames    int
}

func runHighlight(t *testing.T, term *Terminal, config HighlightConfig, seed uint64) highlightRun {
	t.Helper()
	engine := NewEngine(term, NewRng(seed))
	effect := NewHighlight(config)
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}
	run := highlightRun{
		base:      map[*Character]ColorPair{},
		firstLit:  map[*Character]int{},
		brightest: map[*Character]Color{},
		final:     map[*Character]ColorPair{},
	}
	for _, ch := range term.InputCharacters {
		run.base[ch] = ch.Animation.CurrentVisual().Colors
	}
	for frame := 0; frame < 4000; frame++ {
		if !effect.Advance(engine) {
			break
		}
		run.frames++
		for _, ch := range term.InputCharacters {
			colors := ch.Animation.CurrentVisual().Colors
			run.final[ch] = colors
			if colors == run.base[ch] {
				continue
			}
			if _, seen := run.firstLit[ch]; !seen {
				run.firstLit[ch] = frame
			}
			if colors.HasFg && channelSum(colors.Fg) > channelSum(run.brightest[ch]) {
				run.brightest[ch] = colors.Fg
			}
		}
	}
	if run.frames == 0 {
		t.Fatal("the effect produced no frames")
	}
	if run.frames >= 4000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	return run
}

func channelSum(c Color) int { return int(c.R) + int(c.G) + int(c.B) }

// TestHighlightSettlesIntoTheInputText runs highlight to completion and checks
// the text is untouched, since the band only recolours what is already there.
//
// Negative control: pointing the band's frames at a fixed symbol instead of
// ch.InputSymbol leaves the final frame reading as that symbol.
func TestHighlightSettlesIntoTheInputText(t *testing.T) {
	const input = "catch the light"
	term := NewTerminalFromText(input, TerminalConfig{Width: 24, Height: 4})
	engine := NewEngine(term, NewRng(3))

	frames, err := Run(NewHighlight(DefaultHighlightConfig()), engine, 4000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 4000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	for i, frame := range frames {
		rows := nonBlank(frame)
		if len(rows) != 1 || rows[0] != input {
			t.Fatalf("frame %d reads %q, want %q", i, rows, input)
		}
	}
}

// TestHighlightBandSweepsInItsDirection is the test for the thing the effect is
// named for. A highlight is a band of light crossing the screen, so the frame a
// character lights up on has to follow the configured direction. An effect that
// lit every character at once would still resolve correctly and would look
// nothing like a highlight.
//
// The check runs twice: left to right must come out in ascending column order,
// and right to left must not. The second run is the negative control written
// into the test, and the mutation control below was also run by hand.
//
// Negative control: replacing config.HighlightDirection in Build with a fixed
// GroupColumnRightToLeft makes the left-to-right case fail on the first pair of
// columns.
func TestHighlightBandSweepsInItsDirection(t *testing.T) {
	config := highlightGreyConfig()
	config.HighlightDirection = GroupColumnLeftToRight

	term := NewTerminalFromText("abcdefghij", TerminalConfig{Width: 10, Height: 3})
	run := runHighlight(t, term, config, 3)
	forward := highlightColumnOrder(t, term, run)
	if forward == nil {
		return
	}
	for i := 1; i < len(forward); i++ {
		if forward[i].lit < forward[i-1].lit {
			t.Errorf("column %d lit on frame %d, before column %d on frame %d",
				forward[i].column, forward[i].lit, forward[i-1].column, forward[i-1].lit)
		}
	}
	if forward[len(forward)-1].lit <= forward[0].lit {
		t.Errorf("the last column lit on frame %d and the first on frame %d, so the band never travelled",
			forward[len(forward)-1].lit, forward[0].lit)
	}

	config.HighlightDirection = GroupColumnRightToLeft
	term = NewTerminalFromText("abcdefghij", TerminalConfig{Width: 10, Height: 3})
	reverse := highlightColumnOrder(t, term, runHighlight(t, term, config, 3))
	if reverse == nil {
		return
	}
	ascending := true
	for i := 1; i < len(reverse); i++ {
		if reverse[i].lit < reverse[i-1].lit {
			ascending = false
		}
	}
	if ascending {
		t.Error("right to left lit the columns in ascending order too, so the direction is not being read")
	}
}

type highlightColumnLit struct {
	column int
	lit    int
}

// highlightColumnOrder is the first frame anything in each column lit, in
// ascending column order.
func highlightColumnOrder(t *testing.T, term *Terminal, run highlightRun) []highlightColumnLit {
	t.Helper()
	first := map[int]int{}
	for _, ch := range term.InputCharacters {
		lit, ok := run.firstLit[ch]
		if !ok {
			t.Errorf("%q at %v never lit at all", ch.InputSymbol, ch.InputCoord)
			return nil
		}
		if existing, seen := first[ch.InputCoord.Column]; !seen || lit < existing {
			first[ch.InputCoord.Column] = lit
		}
	}
	if len(first) < 2 {
		t.Fatalf("only %d columns held characters, so there is no order to check", len(first))
	}
	out := make([]highlightColumnLit, 0, len(first))
	for column := term.Canvas.Left; column <= term.Canvas.Right; column++ {
		if lit, ok := first[column]; ok {
			out = append(out, highlightColumnLit{column: column, lit: lit})
		}
	}
	return out
}

// TestHighlightBrightensThenReturns checks the band is made of light and not
// just of some other colour: every character reaches exactly the brightness
// adjustment the config asks for, then settles back to where it started.
//
// Negative control: building the band gradient from four copies of the base
// colour instead of {base, bright, bright, base} means no character ever
// reaches the brightened colour, and every character fails the first check.
func TestHighlightBrightensThenReturns(t *testing.T) {
	config := highlightGreyConfig()
	term := NewTerminalFromText("light\nlight", TerminalConfig{Width: 8, Height: 4})
	run := runHighlight(t, term, config, 3)

	for _, ch := range term.InputCharacters {
		base := run.base[ch]
		if !base.HasFg {
			t.Fatalf("%q rests with no foreground, so this test has nothing to measure", ch.InputSymbol)
		}
		want := AdjustColorBrightness(base.Fg, config.HighlightBrightness)
		if channelSum(want) <= channelSum(base.Fg) {
			t.Fatalf("brightness %v does not brighten %v, so this test is vacuous",
				config.HighlightBrightness, base.Fg)
		}
		if got := run.brightest[ch]; got != want {
			t.Errorf("%q at %v peaked at %v, want the brightened %v",
				ch.InputSymbol, ch.InputCoord, got, want)
		}
		if got := run.final[ch]; got != base {
			t.Errorf("%q at %v ended on %v, want its resting %v", ch.InputSymbol, ch.InputCoord, got, base)
		}
	}
}

// TestHighlightPassesOverTheScreen is the check on which kind of effect this
// is. Highlight travels over a picture that is already on the screen; it does
// not assemble one. So under DynamicExistingColors the whole picture, its
// backgrounds included, must be there in the first frame, not appear as the
// band reaches it.
//
// An effect that got this backwards would still resolve correctly and would
// still pass a final-frame check, which is why this is tested on frame one.
//
// Negative control: hiding every character in Build and showing it when the
// sweep releases it makes the first frame blank and the input colours absent.
func TestHighlightPassesOverTheScreen(t *testing.T) {
	red, blue := RGB(255, 0, 0), RGB(0, 0, 255)
	grid := [][]InputCell{{
		{Symbol: "a", Fg: red, HasFg: true, Bg: blue, HasBg: true},
		{Symbol: "b", Fg: red, HasFg: true, Bg: blue, HasBg: true},
	}}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 2, Height: 1, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(3))
	frames, err := Run(NewHighlight(DefaultHighlightConfig()), engine, 4000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if got := plain(frames[0]); got != "ab" {
		t.Errorf("the first frame reads %q, want the whole picture %q", got, "ab")
	}
	if !strings.Contains(frames[0], "\x1b[38;2;255;0;0m") {
		t.Errorf("the first frame does not carry the input's red: %q", frames[0])
	}
	// The band recolours the foreground only, so the background a captured
	// cell arrived with has to be on every frame of the run.
	for i, frame := range frames {
		if !strings.Contains(frame, "\x1b[48;2;0;0;255m") {
			t.Fatalf("frame %d lost the input's background: %q", i, frame)
		}
	}
	last := frames[len(frames)-1]
	if got := plain(last); got != "ab" {
		t.Errorf("the final frame reads %q, want %q", got, "ab")
	}
	if !strings.Contains(last, "\x1b[38;2;255;0;0m") {
		t.Errorf("the final frame does not resolve to the input's red: %q", last)
	}
}

// TestHighlightSweepReleasesEveryGroupOnce pins the easer's truncation. The
// groups are handed out as an eased fraction of the list, and a slice bound
// that was off by one would drop a group or hand one out twice.
//
// Negative control: reading the eased value after the step instead of before,
// so the step is compared against itself, releases no group at all. Run and
// watched to fail with "released 0 groups, want 8".
func TestHighlightSweepReleasesEveryGroupOnce(t *testing.T) {
	term := NewTerminalFromText("abcdefgh", TerminalConfig{Width: 8, Height: 2})
	groups := term.GetCharactersGrouped(InputOnly(), GroupColumnLeftToRight)
	if len(groups) < 2 {
		t.Fatalf("only %d groups, so there is nothing to release", len(groups))
	}
	sweep := &highlightSweep{groups: groups, easing: InOutCirc, totalSteps: highlightSweepSteps}

	var released [][]*Character
	for steps := 0; steps < highlightSweepSteps*2; steps++ {
		released = append(released, sweep.advance()...)
	}
	if !sweep.isComplete() {
		t.Error("the sweep never completed")
	}
	if len(released) != len(groups) {
		t.Fatalf("released %d groups, want %d", len(released), len(groups))
	}
	for i := range groups {
		if released[i][0] != groups[i][0] {
			t.Errorf("group %d was released out of order", i)
		}
	}
}
