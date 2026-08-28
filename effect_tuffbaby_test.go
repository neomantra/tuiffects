package tuiffects

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

// tuffScreenText is a block of text that fills a canvas of the given size.
// A single long line would not: the canvas drops everything past its right
// edge, so a repeat of one line leaves only the first row populated.
func tuffScreenText(width, height int) string {
	line := strings.Repeat("tuffbaby ", width/9+1)[:width]
	return strings.TrimRight(strings.Repeat(line+"\n", height), "\n")
}

// runTuffBaby drives the effect by hand so a test can look at the world on any
// frame and know which phase produced it.
func runTuffBaby(t *testing.T, effect *TuffBaby, e *Engine, inspect func(frame int)) int {
	t.Helper()
	if err := effect.Build(e); err != nil {
		t.Fatalf("Build: %v", err)
	}
	frames := 0
	for effect.Advance(e) {
		frames++
		if inspect != nil {
			inspect(frames)
		}
		if frames > 8000 {
			t.Fatal("the effect never finished within 8000 frames")
		}
	}
	return frames
}

// TestTuffBabyFrameDataDecodes checks the embedded clip before anything else
// leans on it. It is 18KB of base64 that no reviewer can eyeball, so its shape
// and its alphabet are asserted rather than assumed.
//
// Negative control: deleting one line of the base64 makes the decoder panic
// with "frame data is truncated: zlib: invalid checksum". The length check
// below it never gets a chance to fire, which is the right order: a blob that
// does not inflate is not a blob of the wrong size. Run.
func TestTuffBabyFrameDataDecodes(t *testing.T) {
	frames := tuffFrames()
	if len(frames) != tuffFrameCount {
		t.Fatalf("decoded %d frames, want %d", len(frames), tuffFrameCount)
	}
	inked := 0
	for f, grid := range frames {
		if len(grid) != tuffMaskHeight {
			t.Fatalf("frame %d has %d rows, want %d", f, len(grid), tuffMaskHeight)
		}
		for r, row := range grid {
			if len(row) != tuffMaskWidth {
				t.Fatalf("frame %d row %d is %d wide, want %d", f, r, len(row), tuffMaskWidth)
			}
			for _, c := range []byte(row) {
				if c == tuffGround {
					continue
				}
				if c < '1' || c > '0'+tuffToneCount {
					t.Fatalf("frame %d row %d holds %q, want '.' or 1..%d", f, r, c, tuffToneCount)
				}
				inked++
			}
		}
	}
	// A blob that decoded to the right shape but all ground would pass
	// everything above and draw nothing.
	if inked == 0 {
		t.Fatal("the clip is entirely ground, so there is no picture in it")
	}
	// Decoding twice must give the same slice back, since it is cached.
	if &frames[0] != &tuffFrames()[0] {
		t.Error("tuffFrames decoded the blob twice")
	}
}

// TestTuffBabyPutsTheScreenBack runs the effect to completion over text that is
// far too short to fill the picture, so the run is dominated by the characters
// the effect appends. The screen it leaves behind has to be the screen it was
// handed: the appended characters are not part of the input and must not still
// be standing on it.
//
// Negative control: not giving the appended characters a home path at all
// leaves the whole picture standing on the last frame. Run.
//
// The control that does *not* fail is worth naming: hiding the appended
// characters in a sweep after the run, rather than as each one arrives, passes.
// An appended character goes home to the cell it was created on, which is one
// past a canvas edge, and the painter never draws a coordinate off the canvas.
// The hiding is what keeps them out of the visible set, not what keeps them off
// the screen.
func TestTuffBabyPutsTheScreenBack(t *testing.T) {
	const input = "tuff"
	term := NewTerminalFromText(input, TerminalConfig{Width: 60, Height: 20})
	engine := NewEngine(term, NewRng(11))

	frames, err := Run(NewTuffBaby(DefaultTuffBabyConfig()), engine, 8000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	if len(frames) >= 8000 {
		t.Fatal("the effect never finished within the frame cap")
	}
	rows := nonBlank(frames[len(frames)-1])
	if len(rows) != 1 || strings.TrimSpace(rows[0]) != input {
		t.Errorf("the final frame reads %q, want %q", rows, input)
	}
}

// TestTuffBabyStandsUpTheFirstFrame is the test for the picture itself. An
// effect that gathers the text into an arbitrary blob resolves correctly and
// passes every other test here.
//
// It checks the pose against the clip's own first frame: every character whose
// cell that frame covers is standing on its cell wearing that cell's tone, and
// every character whose cell it does not cover is off the screen. The tones
// used have to be more than one, or the picture is a flat silhouette.
//
// Negative control: painting every covered cell with Tones[0] instead of its
// own level reports a level 3 cell wearing {74 64 56}, want {164 143 124}. Run.
func TestTuffBabyStandsUpTheFirstFrame(t *testing.T) {
	config := DefaultTuffBabyConfig()
	term := NewTerminalFromText(tuffScreenText(100, 30), TerminalConfig{Width: 100, Height: 30})
	engine := NewEngine(term, NewRng(5))
	effect := NewTuffBaby(config)

	posed := false
	seen := map[Color]int{}
	lit, dark := 0, 0
	runTuffBaby(t, effect, engine, func(int) {
		if effect.phase != tuffPosing || posed {
			return
		}
		posed = true
		for i := range effect.placed {
			p := &effect.placed[i]
			level := p.tones[0]
			if level == 0 {
				dark++
				if p.ch.IsVisible {
					t.Fatalf("a cell the first frame does not cover is on screen at %v",
						p.ch.Motion.CurrentCoord)
				}
				continue
			}
			lit++
			if !p.ch.IsVisible {
				t.Fatalf("a cell the first frame covers is off screen at %v", p.ch.Motion.CurrentCoord)
			}
			want := config.Tones[level-1]
			got := p.ch.Animation.CurrentVisual().Colors
			if got.Fg != want {
				t.Fatalf("a level %d cell is wearing %v, want %v", level, got.Fg, want)
			}
			seen[want]++
		}
	})

	if !posed {
		t.Fatal("the effect never reached the pose")
	}
	if lit == 0 || dark == 0 {
		t.Fatalf("the first frame covered %d cells and left %d, want both", lit, dark)
	}
	if len(seen) < 3 {
		t.Errorf("the picture used %d of %d tones, want a tonal picture not a silhouette",
			len(seen), tuffToneCount)
	}
}

// TestTuffBabyPlaysTheClip is the test for the thing this effect is now named
// for. The picture has to animate, and it has to animate as the clip does:
// every one of the source's frames reaches the screen, in order, and the
// silhouette moves with them.
//
// Negative control: never advancing t.frame in play() holds frame 0 forever.
// The loop counter is driven by the frame wrapping, so the run never ends and
// this fails on the 8000 frame cap rather than on the count. That is a
// stronger failure than the one intended, not a weaker one. Run.
func TestTuffBabyPlaysTheClip(t *testing.T) {
	term := NewTerminalFromText(tuffScreenText(100, 30), TerminalConfig{Width: 100, Height: 30})
	engine := NewEngine(term, NewRng(4))
	effect := NewTuffBaby(DefaultTuffBabyConfig())

	paintings := map[string]bool{}
	silhouettes := map[string]bool{}
	var last string
	runTuffBaby(t, effect, engine, func(int) {
		if effect.phase != tuffPosing && effect.phase != tuffPlaying {
			return
		}
		var painting, silhouette strings.Builder
		for i := range effect.placed {
			p := &effect.placed[i]
			if !p.visible {
				painting.WriteByte('.')
				silhouette.WriteByte('.')
				continue
			}
			painting.WriteByte('0' + p.tones[effect.frame])
			silhouette.WriteByte('#')
		}
		if painting.String() != last {
			last = painting.String()
			paintings[last] = true
		}
		silhouettes[silhouette.String()] = true
	})

	if len(paintings) != tuffFrameCount {
		t.Errorf("the run painted %d distinct frames, want all %d of the clip",
			len(paintings), tuffFrameCount)
	}
	// Tones changing on a fixed set of cells would be a picture flickering in
	// place. The silhouette has to move too, which is what carries the motion.
	if len(silhouettes) < 10 {
		t.Errorf("the silhouette took %d shapes over the clip, want it moving", len(silhouettes))
	}
}

// TestTuffBabyHoldsEachSourceFrame checks the clip runs at its own speed
// rather than at the host's, at whatever speed the host runs.
//
// The source is ten frames a second, so one of its frames is held for a tenth
// of a second of engine time: six frames on a host painting sixty, twelve on
// one painting a hundred and twenty, twenty-four on one painting two hundred
// and forty. The clip therefore lasts the same 5.5 seconds on all three, and
// all fifty-five of its frames reach the screen on all three.
//
// Counting host frames instead holds every source frame for six of them
// whatever the host does, which is what the fault was: the clip played four
// times too fast on a host at two hundred and forty, at forty source frames a
// second, and a reader saw a blur rather than the clip.
//
// Negative control: putting the host-frame count back in play() (a frameTick
// counter against a hold of six, in place of the clock reading) plays two
// loops over 661 host frames at every rate, so the 120 case reports 5.500
// seconds of clip against the 11 wanted and the 240 case reports 2.750. The 60
// case still passes, which is the point of running all three. Run.
//
// Second control, for the count of frames reached: taking the frame modulo
// five fewer than the clip has reports "the run reached 51 of the clip's
// frames, want all 55" at every rate, while the length of the run is
// untouched. The two assertions fail apart, which is what says they measure
// different things. Run.
func TestTuffBabyHoldsEachSourceFrame(t *testing.T) {
	for _, rate := range []int{60, 120, 240} {
		t.Run(strconv.Itoa(rate), func(t *testing.T) {
			config := DefaultTuffBabyConfig()
			config.Loops = 2
			term := NewTerminalFromText(tuffScreenText(60, 20), TerminalConfig{Width: 60, Height: 20})
			engine := NewEngine(term, NewRng(3))
			engine.Clock = NewVirtualClock(rate)
			effect := NewTuffBaby(config)

			playing := 0
			seen := map[int]bool{}
			runTuffBaby(t, effect, engine, func(int) {
				if effect.phase != tuffPlaying {
					return
				}
				playing++
				seen[effect.frame] = true
			})

			// Two loops of fifty-five frames, each held a tenth of a second,
			// plus the one frame the phase spends deciding it is done.
			hold := rate / int(tuffSourceFrameRate)
			want := config.Loops*tuffFrameCount*hold + 1
			if playing != want {
				t.Errorf("the clip played over %d host frames, want %d", playing, want)
			}
			// The number that matters to a reader: how long the clip is on the
			// screen. It has to be the same on every host.
			seconds := float64(playing-1) / float64(rate)
			if wantSeconds := float64(config.Loops*tuffFrameCount) / tuffSourceFrameRate; seconds != wantSeconds {
				t.Errorf("the clip lasted %.3fs, want %.3fs", seconds, wantSeconds)
			}
			if len(seen) != tuffFrameCount {
				t.Errorf("the run reached %d of the clip's frames, want all %d",
					len(seen), tuffFrameCount)
			}
		})
	}
}

// TestTuffBabyAppendsWhenTheTextRunsOut is the second half of what the effect
// promises: a screen with four characters on it still produces a whole
// picture, not four cells of one.
//
// It also checks what the appended characters are wearing as symbols. Filling
// the picture with a hard-coded glyph would fill it just as completely, and
// would stop it reading as being made out of the text that was there.
//
// Negative control: dropping the AddCharacter branch and breaking out of the
// layout loop once the input runs out reports 1 of 1452 cells filled and 0
// characters appended, so the picture never appears. Run.
func TestTuffBabyAppendsWhenTheTextRunsOut(t *testing.T) {
	term := NewTerminalFromText("tuff", TerminalConfig{Width: 90, Height: 28})
	engine := NewEngine(term, NewRng(2))
	effect := NewTuffBaby(DefaultTuffBabyConfig())
	if err := effect.Build(engine); err != nil {
		t.Fatalf("Build: %v", err)
	}

	cells := effect.layout(term.Canvas)
	if len(cells) < 100 {
		t.Fatalf("the picture is only %d cells on a 90 by 28 canvas", len(cells))
	}
	if len(effect.placed) != len(cells) {
		t.Errorf("%d of %d cells were filled", len(effect.placed), len(cells))
	}
	if want := len(cells) - 4; len(effect.added) != want {
		t.Errorf("the effect appended %d characters, want %d", len(effect.added), want)
	}

	own := map[string]bool{}
	for _, ch := range term.InputCharacters {
		own[ch.InputSymbol] = true
	}
	for _, ch := range effect.added {
		if !own[ch.InputSymbol] {
			t.Fatalf("an appended character wears %q, which is not on the screen", ch.InputSymbol)
		}
	}

	// And the four real ones are spread through the picture rather than piled
	// into the first four cells, which is what a straight zip would do.
	// Build has not advanced anything yet, so a character is still standing
	// where it came from; the cell it was given is the gather flight's target.
	var rows []int
	for i := range effect.placed {
		for _, ch := range term.InputCharacters {
			if effect.placed[i].ch == ch {
				rows = append(rows, ch.Motion.Path("gather").Waypoints[0].Coord.Row)
			}
		}
	}
	if len(rows) != 4 {
		t.Fatalf("%d of the 4 input characters made it into the picture", len(rows))
	}
	span := 0
	for _, row := range rows {
		span = max(span, rows[0]-row)
	}
	if span < 10 {
		t.Errorf("the input characters span %d rows of the picture, want them spread through it", span)
	}
}

// TestTuffBabyClearsTheSurplusAndBringsItBack covers the other direction, which
// is the one a screen saver runs in: a full screen carries far more characters
// than the picture has cells, and the ones left over have to get out of the
// way rather than stand in front of it.
//
// Negative control: pointing a surplus character's gather path at its own input
// coordinate instead of at tuffNearestExit reports one still standing at {3 30}
// on the first frame of the pose, which is the whole screen showing through the
// picture. Run.
func TestTuffBabyClearsTheSurplusAndBringsItBack(t *testing.T) {
	term := NewTerminalFromText(tuffScreenText(100, 30), TerminalConfig{Width: 100, Height: 30})
	engine := NewEngine(term, NewRng(8))
	effect := NewTuffBaby(DefaultTuffBabyConfig())

	posed := false
	runTuffBaby(t, effect, engine, func(int) {
		if effect.phase != tuffPosing || posed {
			return
		}
		posed = true
		if len(effect.parked) == 0 {
			t.Fatal("a full screen produced no surplus, so nothing is being tested")
		}
		for _, ch := range effect.parked {
			if term.Canvas.CoordIsInCanvas(ch.Motion.CurrentCoord) {
				t.Fatalf("a surplus character is still at %v during the pose", ch.Motion.CurrentCoord)
			}
		}
	})

	if !posed {
		t.Fatal("the effect never reached the pose")
	}
	for _, ch := range effect.parked {
		if ch.Motion.CurrentCoord != ch.InputCoord {
			t.Errorf("a surplus character finished at %v, not back at %v",
				ch.Motion.CurrentCoord, ch.InputCoord)
		}
	}
}

// TestTuffBabySampleDecidesCoverageBeforeTone pins the downscale rule.
//
// A target cell straddling the edge of the picture covers both ground and ink.
// Averaging tone across the whole block treats ground as a sixth, darkest
// level, so every edge cell comes out dark and the figure wears a ring of
// shadow that is not in the source. Coverage is decided first, on its own, and
// the tone is then averaged over the inked cells only.
//
// Negative control: counting ground as level 0 in the average drops the tone of
// this half-covered block from 4 to 2. Run.
func TestTuffBabySampleDecidesCoverageBeforeTone(t *testing.T) {
	// A block whose left half is ground and whose right half is level 4. Laid
	// out so one target cell covers the whole width of the source.
	grid := make([]string, tuffMaskHeight)
	for row := range grid {
		grid[row] = strings.Repeat(".", tuffMaskWidth/2) + strings.Repeat("4", tuffMaskWidth/2)
	}
	if got := tuffSample(grid, 0, 0, 1, 1); got != 4 {
		t.Errorf("a half-covered block of level 4 sampled as %d, want 4", got)
	}

	// Below the coverage threshold the cell is not part of the picture at all.
	sparse := make([]string, tuffMaskHeight)
	for row := range sparse {
		sparse[row] = strings.Repeat(".", tuffMaskWidth-4) + "4444"
	}
	if got := tuffSample(sparse, 0, 0, 1, 1); got != 0 {
		t.Errorf("a block covered well under half sampled as %d, want 0", got)
	}
}

// TestTuffBabyResolvesTheInputColours runs the effect under the colour policy a
// screen saver uses. The picture is the effect's own palette; the screen it
// puts back has to be the screen's.
//
// Negative control: dropping the DynamicExistingColors branch from finalColors
// leaves the final frame carrying the configured gradient, {140 74 21} on the
// top row, instead of the input's red. Run.
func TestTuffBabyResolvesTheInputColours(t *testing.T) {
	red := RGB(255, 0, 0)
	grid := make([][]InputCell, 8)
	for y := range grid {
		row := make([]InputCell, 30)
		for x := range row {
			row[x] = InputCell{Symbol: "t", Fg: red, HasFg: true}
		}
		grid[y] = row
	}
	term := NewTerminalFromCells(grid, TerminalConfig{
		Width: 30, Height: 8, ExistingColorHandling: DynamicExistingColors,
	})
	engine := NewEngine(term, NewRng(12))

	frames, err := Run(NewTuffBaby(DefaultTuffBabyConfig()), engine, 8000)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("the effect produced no frames")
	}
	last := frames[len(frames)-1]
	if got := strings.Count(plain(last), "t"); got != 30*8 {
		t.Fatalf("the final frame holds %d characters, want %d", got, 30*8)
	}
	if !strings.Contains(last, "\x1b[38;2;255;0;0m") {
		t.Errorf("the final frame does not carry the input's red: %q", last)
	}
}

// TestTuffBabyChecksItsTones guards the one configuration mistake that would
// otherwise be found at paint time, as an index out of range.
//
// Negative control: dropping the guard makes Build panic with "index out of
// range [4] with length 4" while it is dressing the first character, rather
// than returning an error. Run.
func TestTuffBabyChecksItsTones(t *testing.T) {
	term := NewTerminalFromText("tuff", TerminalConfig{Width: 60, Height: 20})
	short := DefaultTuffBabyConfig()
	short.Tones = short.Tones[:tuffToneCount-1]
	if err := NewTuffBaby(short).Build(NewEngine(term, NewRng(1))); err == nil {
		t.Error("a config with too few tones built without complaint")
	}
}

// TestTuffBabyDrawsSomethingOnAnyCanvas pins how far down this scales.
//
// The cells are the union of every frame of the clip, and the subject covers
// most of the source on most frames, so a canvas of any size at all resolves
// to at least one covered cell. There is a guard in Build for an empty picture
// and it is deliberately unreachable by shrinking the canvas; it is there for
// a future reference that does not cover its own frame.
//
// Negative control: no size here can be made to fail by shrinking, which is
// the point of the test. The control that does fail is inverting the coverage
// rule in tuffSample: 1x1 then reports "the canvas is too small to hold the
// picture" and 4x3 divides by zero in the sampler. Run.
func TestTuffBabyDrawsSomethingOnAnyCanvas(t *testing.T) {
	for _, size := range []struct{ width, height int }{{1, 1}, {4, 3}, {8, 4}, {40, 12}, {200, 50}} {
		term := NewTerminalFromText("tuff", TerminalConfig{Width: size.width, Height: size.height})
		effect := NewTuffBaby(DefaultTuffBabyConfig())
		if err := effect.Build(NewEngine(term, NewRng(1))); err != nil {
			t.Errorf("%dx%d: %v", size.width, size.height, err)
			continue
		}
		if len(effect.placed) == 0 {
			t.Errorf("%dx%d built a picture with no cells in it", size.width, size.height)
		}
	}
}

// TestTuffBabyBoldsTheLightEnd guards the one thing that carries the picture
// as much as the palette does.
//
// A glyph fills about a third of its cell, and how much varies from glyph to
// glyph, so over arbitrary text that variation is louder than a five step
// colour ramp. The light end is drawn bold to put the tonal order back. It is
// invisible to every other test here: the colours are right either way.
//
// Negative control: passing false for bold reports the first level 4 cell
// coming back with bold unset. Run.
func TestTuffBabyBoldsTheLightEnd(t *testing.T) {
	term := NewTerminalFromText(tuffScreenText(100, 30), TerminalConfig{Width: 100, Height: 30})
	engine := NewEngine(term, NewRng(9))
	effect := NewTuffBaby(DefaultTuffBabyConfig())

	bold, plainInk := 0, 0
	runTuffBaby(t, effect, engine, func(int) {
		if effect.phase != tuffPlaying {
			return
		}
		for i := range effect.placed {
			p := &effect.placed[i]
			if !p.visible {
				continue
			}
			isBold := p.ch.Animation.CurrentVisual().Bold
			wantBold := p.tones[effect.frame] >= tuffBoldFrom
			if isBold != wantBold {
				t.Fatalf("a level %d cell has bold=%v, want %v", p.tones[effect.frame], isBold, wantBold)
			}
			if isBold {
				bold++
			} else {
				plainInk++
			}
		}
	})

	if bold == 0 || plainInk == 0 {
		t.Fatalf("the picture drew %d bold cells and %d plain, want both", bold, plainInk)
	}
}

// tuffOffTheLine measures how far a coordinate has strayed from the straight
// line between two others, rows counted double the way every other distance in
// this package is.
func tuffOffTheLine(p, from, to Coord) float64 {
	px, py := float64(p.Column), 2*float64(p.Row)
	ax, ay := float64(from.Column), 2*float64(from.Row)
	dx, dy := float64(to.Column)-ax, 2*float64(to.Row)-ay
	if dx == 0 && dy == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	along := math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(px-(ax+along*dx), py-(ay+along*dy))
}

// TestTuffBabySurplusLeavesAtItsOwnSpeed is the fix for what a wide screen
// looked like while the picture was still forming.
//
// A 240 by 56 screen of text hands the picture about 5,800 characters and has
// about 6,200 left over. Flown on the gather's own path options the surplus
// took 46 frames to clear, and because the gather eases out it spent most of
// them sitting near an edge having covered nearly all of its distance in the
// first few: the left and right of the screen carried a readable band of text
// for three quarters of a second, while the middle held nothing recognisable
// yet. It read as debris rather than as motion, and the wider the terminal the
// longer it lasted.
//
// The surplus now leaves on its own speed with no easing, by the straight line
// rather than the gather's arc. This checks all three.
//
// Negative controls, each run and each restored:
//
//   - Flying the surplus at GatherSpeed and GatherEase, which is what
//     buildFlights did before it was told which characters are leaving: 4,894
//     of the 6,204 surplus characters are still standing past their deadline,
//     3,469 of them off the straight line by up to 8 cells, frame 20 carries
//     897 glyphs beside the picture rather than under 500, and 559 of the
//     surplus are in the edge columns at once.
//   - Keeping the exit's own speed and straight line but easing it out again
//     (ExitEase: OutQuint): every character is gone on time, on its line, and
//     frame 20 is quiet, and the band check alone fails at 536. That is the
//     one this effect had wrong. An eased flight covers nearly all of its
//     length at once and creeps the rest, so it is the easing rather than the
//     speed that puts the surplus at the edge and holds it there, and only a
//     count of what is standing at the edge sees it.
//   - Keeping the exit's speed and easing but handing it tuffArc again: the
//     line check alone fails, at 10 cells off.
func TestTuffBabySurplusLeavesAtItsOwnSpeed(t *testing.T) {
	for _, size := range []struct{ width, height int }{{240, 56}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			term := NewTerminalFromText(tuffScreenText(size.width, size.height),
				TerminalConfig{Width: size.width, Height: size.height})
			engine := NewEngine(term, NewRng(7))
			config := DefaultTuffBabyConfig()
			effect := NewTuffBaby(config)
			if err := effect.Build(engine); err != nil {
				t.Fatalf("Build: %v", err)
			}
			canvas := term.Canvas
			if len(effect.parked) == 0 {
				t.Fatal("a full screen produced no surplus, so nothing is being tested")
			}

			// A flight at a constant speed covers its length in length over
			// speed frames, whatever the length. One frame of slack covers the
			// rounding the path does when it works out its step count.
			exit := make(map[*Character]Coord, len(effect.parked))
			deadline := make(map[*Character]int, len(effect.parked))
			for _, ch := range effect.parked {
				at := tuffNearestExit(canvas, ch.InputCoord)
				exit[ch] = at
				deadline[ch] = roundHalfEven(
					FindLengthOfLine(ch.InputCoord, at, true)/config.ExitSpeed) + 1
			}

			late := map[*Character]bool{}
			wandered := map[*Character]bool{}
			worst, outsideAt20, band := 0.0, 0, 0
			for frame := 1; effect.phase == tuffGathering; frame++ {
				if !effect.Advance(engine) {
					break
				}
				standing := 0
				for _, ch := range effect.parked {
					at := ch.Motion.CurrentCoord
					if !canvas.CoordIsInCanvas(at) {
						continue
					}
					if frame > deadline[ch] {
						late[ch] = true
					}
					off := tuffOffTheLine(at, ch.InputCoord, exit[ch])
					if off > 1 {
						wandered[ch] = true
						worst = math.Max(worst, off)
					}
					if at.Column <= canvas.Left+7 || at.Column >= canvas.Right-7 {
						standing++
					}
				}
				band = max(band, standing)
				if frame == 20 {
					outsideAt20 = tuffGlyphsOutsideThePicture(effect, engine)
				}
				if frame > 400 {
					t.Fatal("the gather never ended")
				}
			}

			if len(late) > 0 {
				t.Errorf("%d of %d surplus characters were still on the canvas past the frame "+
					"their own length over ExitSpeed puts them off it", len(late), len(effect.parked))
			}
			if len(wandered) > 0 {
				t.Errorf("%d of %d surplus characters strayed off the straight line to their "+
					"exit, the worst by %.0f cells; the exit is meant to be the straight line",
					len(wandered), len(effect.parked), worst)
			}
			if size.width != 240 {
				return
			}
			// The rest is the screen the report was made on, where the band
			// was seen. The picture is still forming at frame 20 whatever the
			// surplus does; what has to be gone by then is everything beside
			// it.
			if outsideAt20 >= 500 {
				t.Errorf("frame 20 carries %d glyphs outside the picture's columns, want under 500",
					outsideAt20)
			}
			// How thick the band gets while it drains, counted over the eight
			// columns at each edge the report described. A flight that eases
			// out arrives near the edge at once and then creeps, so the
			// surplus collects there whatever its speed; an even one does not.
			if band >= 400 {
				t.Errorf("%d surplus characters stood in the eight columns at an edge at once, "+
					"want under 400", band)
			}
		})
	}
}

// tuffGlyphsOutsideThePicture counts the visible glyphs standing in a column
// the picture does not reach, which is the count the report was made on.
func tuffGlyphsOutsideThePicture(effect *TuffBaby, e *Engine) int {
	left, right := e.Terminal.Canvas.Right, e.Terminal.Canvas.Left
	for _, cell := range effect.layout(e.Terminal.Canvas) {
		left = min(left, cell.coord.Column)
		right = max(right, cell.coord.Column)
	}
	count := 0
	for _, row := range e.FrameRows() {
		for column, visual := range row {
			if column+1 >= left && column+1 <= right {
				continue
			}
			if visual == nil || strings.TrimSpace(visual.Symbol) == "" {
				continue
			}
			count++
		}
	}
	return count
}
