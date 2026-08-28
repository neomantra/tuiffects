package tuiffects

// tuffbaby is the one effect in this package that is not a port. It has no
// ttfx source and no TerminalTextEffects source: neither project has an effect
// like it and no code was taken from either one for it. It runs on this
// package's ported engine, which is ChrisBuilds' design, the same way every
// port does.
//
// It is credited in its Descriptor's Origin rather than in NOTICE, because
// NOTICE is a record of what was translated and from where, and this was not
// translated. catalogue_test.go holds every effect to one or the other and
// refuses both.
//
// The likeness is not this package's either. It is derived from a GIF
// published under Tenor's "tuff baby" tag, 55 frames, 498 by 498, greyscale:
// https://media1.tenor.com/m/3UK3aqKDXwQAAAAC/tuff-baby-ai-baby.gif
//
// What ships is a derivative, not a copy: tuffbaby_frames.go holds each frame
// reduced to a 72 by 36 grid of one character per terminal cell carrying one
// of five brightness levels, which is roughly a thousandth of the source's
// pixels and none of its colour. It is still a reduction of someone else's
// footage, so the source is named rather than left implied.
//
// It arranges whatever text is already on the screen into a portrait of the
// "tuff baby" meme and then plays the meme, one tone per cell, before putting
// every character back where it came from. Where the screen does not carry
// enough characters to fill the picture, the effect appends more.
//
// The picture is a fixed grid of cells and the animation is per-cell: nothing
// moves during playback, the tones change. That is what playing a clip on a
// character grid is, and it is why the motion here is the source's own rather
// than something invented to sit on top of a still.

import (
	"fmt"
	"math"
	"sort"
)

func init() {
	Register(Descriptor{
		Name:        "tuffbaby",
		Description: "Gathers the text into a portrait of tuff baby, plays the clip in it, then sends it home",
		New:         func() Effect { return NewTuffBaby(DefaultTuffBabyConfig()) },
		Origin: "original to this package; the clip is reduced from a GIF under " +
			"Tenor's \"tuff baby\" tag, " +
			"https://media1.tenor.com/m/3UK3aqKDXwQAAAAC/tuff-baby-ai-baby.gif",
	})
}

// TuffBabyConfig tunes the tuffbaby effect.
type TuffBabyConfig struct {
	// GatherEase and GatherSpeed shape the flight from the text into the
	// picture. HomeEase and HomeSpeed shape the flight back.
	GatherEase  Easing
	GatherSpeed float64
	HomeEase    Easing
	HomeSpeed   float64

	// ExitEase and ExitSpeed shape the surplus's flight off the screen.
	//
	// The surplus is not part of the picture and on a wide screen there is
	// more of it than there is picture: 240 columns of text hands the picture
	// about five thousand characters and leaves about six thousand over.
	// Flown at the gather's speed and curve, that surplus takes three
	// quarters of a second to clear, and it does not clear evenly. The
	// gather eases out, so a character covers most of its distance at once
	// and then creeps the last of the way; applied to a flight whose target
	// is an edge, that puts every surplus character near an edge early and
	// leaves it there. The left and right of the screen carry a readable
	// band of text for the whole of that time, while the picture in the
	// middle is still nothing anyone can recognise. The wider the terminal
	// the further the far characters have to come and the longer the band
	// lasts, which is why it reads as debris rather than as motion.
	//
	// So the surplus leaves at its own speed and, by default, without the
	// gather's easing. The surplus is draining off the edges of the screen
	// and a drain wants an even flux: at a constant rate nothing queues up
	// at the door, and the band that forms is both thinner than the gather's
	// and gone in a third of the time. The step per frame stays what the
	// gather's own opening frames are, so it still reads as flight.
	//
	// It also flies the straight line rather than the gather's arc. The arc
	// turns a character into the picture, which is what a character joining
	// the picture is doing; a character leaving is only getting out of the
	// way, and an arc there is a detour across the thing the reader is
	// trying to watch form.
	//
	// A speed of zero or less falls back to GatherSpeed, GatherEase and the
	// arc, so a config built by hand before these fields existed behaves
	// exactly as it did.
	ExitEase  Easing
	ExitSpeed float64

	// PoseFrames is how long the first frame of the clip is held before it
	// starts playing, so a reader gets to see what the picture is.
	PoseFrames int
	// Loops is how many times the clip plays.
	Loops int
	// SourceFrameRate is how many of the clip's frames play per second of
	// engine time. The source runs at ten, so ten plays it at its own speed.
	//
	// It is read against Engine.Clock rather than against a count of host
	// frames, so the clip lasts the same number of seconds whether the host
	// paints sixty frames a second or two hundred and forty. A host that
	// leaves the engine's clock at a rate it does not paint at gets the clip
	// at the wrong speed, which is the clock's contract, not this field's.
	SourceFrameRate float64

	// FillerSymbols are the symbols appended characters wear when the screen
	// carries no text of its own to recycle.
	FillerSymbols []string

	// Tones colour the picture, darkest first. There must be exactly
	// tuffToneCount of them, matching the levels the reference was reduced to.
	//
	// The defaults are not the source's own greys. The source is a light
	// picture on a white ground and a terminal is the other way up: the
	// darkest band painted as the near-black it really is takes the whole
	// shadowed side of the face out. The ramp keeps the source's tonal order
	// and lifts its dark end until every band reads against a dark terminal.
	Tones []Color

	// FinalGradientStops colour the text once it is home again. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultTuffBabyConfig is the effect as it is meant to be seen.
func DefaultTuffBabyConfig() TuffBabyConfig {
	return TuffBabyConfig{
		GatherEase:      OutQuint,
		GatherSpeed:     0.7,
		ExitEase:        Linear,
		ExitSpeed:       tuffExitSpeed,
		HomeEase:        InOutQuad,
		HomeSpeed:       0.6,
		PoseFrames:      12,
		Loops:           1,
		SourceFrameRate: tuffSourceFrameRate,
		FillerSymbols:   []string{"t", "u", "f", "f"},
		Tones: []Color{
			MustParseColor("241f1c"),
			MustParseColor("55402c"),
			MustParseColor("9a6f42"),
			MustParseColor("d8b071"),
			MustParseColor("fff8ec"),
		},

		FinalGradientStops: []Color{
			MustParseColor("ffe9b8"), MustParseColor("e8a33d"), MustParseColor("8c4a15"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// The numbers that shape the effect but are not worth a knob.
const (
	// tuffToneCount is how many ink levels the reference was reduced to.
	// Level 0 is the ground the picture is not drawn on.
	tuffToneCount = 5
	// tuffRampSteps is the length of the colour ramp into and out of the
	// picture.
	tuffRampSteps = 10
	// tuffSettleDuration holds each frame of the ramp home for two ticks, so
	// the colour change is readable against a flight that is over quickly.
	tuffSettleDuration = 2
	// tuffFlightLayer is the layer a character is lifted to while it is in
	// the air, so it passes in front of the ones that have already landed.
	tuffFlightLayer = 1
	// tuffBoldFrom is the tone at which a cell is drawn bold. See tuffSetTone.
	tuffBoldFrom = 4
	// tuffExitSpeed is the default ExitSpeed, in cells per frame. It is a
	// little over four times GatherSpeed, which is what makes the surplus's
	// longest flight on a 240 by 56 screen take about a third of a second
	// rather than three quarters of one. It is not chosen to be as fast as
	// possible: at this rate a surplus character steps about four cells a
	// frame, which is what the gather's own first frames step, so the
	// surplus still reads as flight rather than as glyphs blinking out.
	tuffExitSpeed = 3.0
	// tuffSourceFrameRate is the rate the reference clip was reduced at, and
	// so the rate SourceFrameRate defaults to.
	tuffSourceFrameRate = 10.0
	// tuffClockNudge is added to the clock reading before the clip's frame is
	// worked out from it, so a boundary the clock has reached to within a
	// rounding error counts as reached.
	//
	// A virtual clock adds its step once per frame, and after a few hundred
	// frames that sum sits a few parts in ten thousand billion below the exact
	// multiple: at sixty frames a second the sixth step lands on 0.0999999...
	// rather than on 0.1, and without the nudge every source frame would be
	// held one host frame too long. A microsecond is far larger than that
	// drift and far smaller than any host's frame.
	tuffClockNudge = 1e-6
	// tuffCoverage is the share of a target cell's source block that has to
	// be part of the picture before the cell is. Half keeps the silhouette
	// where the source put it; averaging tone across the edge instead would
	// ring the whole figure in a band of mid grey.
	tuffCoverage = 0.5
)

// tuffGround is the tone character for a cell that is not part of the picture.
const tuffGround byte = '.'

// tuffCell is one cell of the scaled picture.
type tuffCell struct {
	coord Coord
	// tones is this cell's ink level in every source frame, 0 for the frames
	// the picture does not cover it.
	tones []byte
}

// tuffPlacement is a character standing in a cell of the picture.
type tuffPlacement struct {
	ch *Character
	// carried is the background the character brought in with it, held so
	// every repaint during playback puts the tone back on it rather than
	// dropping it.
	carried ColorPair
	tones   []byte
	visible bool
}

type tuffPhase int

const (
	tuffGathering tuffPhase = iota
	tuffPosing
	tuffPlaying
	tuffGoingHome
)

// TuffBaby gathers the text on screen into a portrait, plays the clip in it,
// and puts the text back.
//
// The characters that make up the picture are the ones that were already
// there. When there are more than the picture needs, the surplus flies off the
// nearest edge and waits there; when there are fewer, the effect appends
// characters of its own, which fly in from off screen and leave the same way.
//
// The cells are the union of every frame, so a character keeps one cell for
// the whole run. A frame that does not cover its cell hides it, which is how
// the silhouette moves without anything moving.
type TuffBaby struct {
	config TuffBabyConfig

	// placed are the characters standing in the picture, parked are the
	// surplus waiting off screen, and added are the ones this effect created.
	// An added character is in both placed and added.
	placed []tuffPlacement
	parked []*Character
	added  []*Character

	phase         tuffPhase
	poseRemaining int
	frame         int

	// playStart is the clock reading the clip started from, playFrames is how
	// many source frames the whole run plays, counting every loop, and
	// clipDone says the last of them has been reached.
	playStart  float64
	playFrames int
	clipDone   bool
}

// NewTuffBaby builds the effect.
func NewTuffBaby(config TuffBabyConfig) *TuffBaby {
	return &TuffBaby{config: config}
}

// Build scales the picture to the canvas, hands out its cells, and starts
// every character on its way there.
func (t *TuffBaby) Build(e *Engine) error {
	if len(t.config.Tones) != tuffToneCount {
		return fmt.Errorf("tuiffects: tuffbaby needs exactly %d tones, got %d",
			tuffToneCount, len(t.config.Tones))
	}
	if e.Clock == nil {
		// The clip is paced by the clock, and a clock that never moves would
		// hold the first frame for ever. NewEngine installs one; this is for
		// an engine assembled by hand.
		e.Clock = NewVirtualClock(60)
	}
	cells := t.layout(e.Terminal.Canvas)
	if len(cells) == 0 {
		return fmt.Errorf("tuiffects: the canvas is too small to hold the picture")
	}

	finalColors, err := t.finalColors(e)
	if err != nil {
		return err
	}
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	chosen, parked := t.chooseCharacters(e, len(cells))
	t.parked = parked
	t.placed = make([]tuffPlacement, 0, len(cells))
	t.added = nil

	filler := t.fillerSymbols(e)
	fillerIndex, taken := 0, 0
	for i, cell := range cells {
		// Hand the real characters out at an even stride through the picture
		// rather than filling it from the top, so a single line of text reads
		// across the whole figure instead of disappearing into its scalp.
		var ch *Character
		if len(chosen) > 0 && i*len(chosen)/len(cells) == taken {
			ch = chosen[taken]
			taken++
		} else {
			ch = e.Terminal.AddCharacter(
				filler[fillerIndex%len(filler)],
				e.Terminal.Canvas.RandomCoord(e.Rng, true, false))
			fillerIndex++
			t.added = append(t.added, ch)
		}
		carried, err := t.dressForPicture(ch, cell.tones, finalColors[ch], dynamic)
		if err != nil {
			return err
		}
		if err := t.buildFlights(ch, cell.coord, ch.InputCoord, finalColors[ch], false); err != nil {
			return err
		}
		t.placed = append(t.placed, tuffPlacement{ch: ch, carried: carried, tones: cell.tones})
	}

	for _, ch := range t.parked {
		exit := tuffNearestExit(e.Terminal.Canvas, ch.InputCoord)
		if err := t.dressForExit(ch, finalColors[ch], dynamic); err != nil {
			return err
		}
		if err := t.buildFlights(ch, exit, ch.InputCoord, finalColors[ch], true); err != nil {
			return err
		}
	}

	for _, ch := range e.Terminal.CollectCharacters(CharacterFilter{Input: true, Added: true}) {
		e.Terminal.SetCharacterVisibility(ch, true)
		e.ActivateScene(ch, "gatherRamp")
		e.Activate(ch)
		e.ActivatePath(ch, "gather")
	}
	for i := range t.placed {
		t.placed[i].visible = true
	}

	t.phase = tuffGathering
	t.poseRemaining = max(t.config.PoseFrames, 0)
	t.frame, t.playStart = 0, 0
	t.playFrames = max(t.config.Loops, 0) * tuffFrameCount
	t.clipDone = t.playFrames == 0
	return nil
}

// layout scales the picture to the canvas and returns the union of every
// frame's cells, in reading order, top row first.
func (t *TuffBaby) layout(canvas *Canvas) []tuffCell {
	scale := math.Min(float64(canvas.Width)/tuffMaskWidth, float64(canvas.Height)/tuffMaskHeight)
	width := min(max(roundHalfEven(tuffMaskWidth*scale), 1), canvas.Width)
	height := min(max(roundHalfEven(tuffMaskHeight*scale), 1), canvas.Height)

	left := canvas.Left + (canvas.Width-width)/2
	bottom := canvas.Bottom + (canvas.Height-height)/2

	frames := tuffFrames()
	cells := make([]tuffCell, 0, width*height)
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			tones := make([]byte, tuffFrameCount)
			covered := false
			for f, grid := range frames {
				tones[f] = tuffSample(grid, column, row, width, height)
				covered = covered || tones[f] != 0
			}
			if !covered {
				continue
			}
			cells = append(cells, tuffCell{
				// Source row 0 is the top of the picture and canvas rows count
				// up from the bottom.
				coord: C(left+column, bottom+height-1-row),
				tones: tones,
			})
		}
	}
	return cells
}

// tuffSample reduces the block of one source frame covering one target cell to
// a single ink level, returning 0 for a cell the picture does not cover.
//
// Coverage is decided first and tone second. Averaging tone straight across
// the block would blend the ground into the edge and ring the whole figure in
// mid grey; deciding coverage on its own keeps the silhouette crisp, and
// averaging only over the cells that are actually part of the picture keeps
// the interior's gradients.
func tuffSample(grid []string, column, row, width, height int) byte {
	firstColumn := column * tuffMaskWidth / width
	lastColumn := max((column+1)*tuffMaskWidth/width, firstColumn+1)
	firstRow := row * tuffMaskHeight / height
	lastRow := max((row+1)*tuffMaskHeight/height, firstRow+1)

	total, inked, sum := 0, 0, 0
	for y := firstRow; y < lastRow && y < tuffMaskHeight; y++ {
		line := grid[y]
		for x := firstColumn; x < lastColumn && x < tuffMaskWidth; x++ {
			total++
			if line[x] == tuffGround {
				continue
			}
			inked++
			sum += int(line[x] - '0')
		}
	}
	if total == 0 || float64(inked) < tuffCoverage*float64(total) {
		return 0
	}
	level := (sum + inked/2) / inked
	return byte(min(max(level, 1), tuffToneCount))
}

// toneColors is what a character in this cell wears on a given frame, on
// whatever background it carried in. A frame the picture does not cover
// reports false and the character is hidden for it.
func (t *TuffBaby) toneColors(p *tuffPlacement, frame int) (ColorPair, bool) {
	level := p.tones[frame]
	if level == 0 {
		return ColorPair{}, false
	}
	colors := Fg(t.config.Tones[level-1])
	if p.carried.HasBg {
		colors.Bg, colors.HasBg = p.carried.Bg, true
	}
	return colors, true
}

// chooseCharacters splits the input into the ones that will make up the
// picture and the surplus that will get out of the way.
//
// Characters carrying a visible glyph are preferred, because a picture drawn
// out of the blank cells of a captured screen is a picture of nothing. Where
// there are more candidates than cells they are sampled at an even stride
// across the screen rather than taken from the top, so the picture is drawn
// out of the whole screen.
func (t *TuffBaby) chooseCharacters(e *Engine, cells int) (chosen, parked []*Character) {
	var inked, blank []*Character
	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		if ch.InputSymbol == " " || ch.InputSymbol == "" {
			blank = append(blank, ch)
			continue
		}
		inked = append(inked, ch)
	}

	chosen = tuffStride(inked, min(len(inked), cells))
	if len(chosen) < cells {
		chosen = append(chosen, tuffStride(blank, min(len(blank), cells-len(chosen)))...)
	}
	sort.SliceStable(chosen, func(i, j int) bool {
		a, b := chosen[i].InputCoord, chosen[j].InputCoord
		if a.Row != b.Row {
			return a.Row > b.Row
		}
		return a.Column < b.Column
	})

	taken := make(map[*Character]struct{}, len(chosen))
	for _, ch := range chosen {
		taken[ch] = struct{}{}
	}
	for _, ch := range e.Terminal.InputCharacters {
		if _, ok := taken[ch]; !ok {
			parked = append(parked, ch)
		}
	}
	return chosen, parked
}

// tuffStride picks want characters spread evenly through list.
func tuffStride(list []*Character, want int) []*Character {
	if want >= len(list) {
		return append([]*Character(nil), list...)
	}
	out := make([]*Character, 0, want)
	for i := 0; i < want; i++ {
		out = append(out, list[i*len(list)/want])
	}
	return out
}

// fillerSymbols is what an appended character wears. The screen's own glyphs
// come first, so the picture still reads as being made of the text that was
// there; the configured symbols are the fallback for a screen with no text on
// it at all.
func (t *TuffBaby) fillerSymbols(e *Engine) []string {
	var symbols []string
	for _, ch := range e.Terminal.InputCharacters {
		if ch.InputSymbol != " " && ch.InputSymbol != "" {
			symbols = append(symbols, ch.InputSymbol)
		}
	}
	if len(symbols) > 0 {
		return symbols
	}
	if len(t.config.FillerSymbols) > 0 {
		return t.config.FillerSymbols
	}
	return []string{"*"}
}

// tuffNearestExit is the cell just past whichever canvas edge is closest. Rows
// count double, because a cell is about twice as tall as it is wide and the
// nearest edge by eye is not the nearest by cell count.
func tuffNearestExit(canvas *Canvas, at Coord) Coord {
	left := at.Column - canvas.Left
	right := canvas.Right - at.Column
	down := 2 * (at.Row - canvas.Bottom)
	up := 2 * (canvas.Top - at.Row)
	switch shortest := min(min(left, right), min(down, up)); shortest {
	case left:
		return C(canvas.Left-1, at.Row)
	case right:
		return C(canvas.Right+1, at.Row)
	case down:
		return C(at.Column, canvas.Bottom-1)
	default:
		return C(at.Column, canvas.Top+1)
	}
}

// finalColors is what every character settles on once it is home: the colour
// it arrived wearing under DynamicExistingColors, and the configured gradient
// otherwise.
func (t *TuffBaby) finalColors(e *Engine) (map[*Character]ColorPair, error) {
	gradient, err := NewGradient(t.config.FinalGradientStops, t.config.FinalGradientSteps, false)
	if err != nil {
		return nil, err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		t.config.FinalGradientDirection)
	if err != nil {
		return nil, err
	}
	fallback := gradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	colors := make(map[*Character]ColorPair, len(e.Terminal.InputCharacters))
	for _, ch := range e.Terminal.InputCharacters {
		if dynamic {
			colors[ch] = ch.Animation.InputColors
			continue
		}
		colors[ch] = Fg(mapping.At(ch.InputCoord, fallback))
	}
	return colors, nil
}

// dressForPicture gives a character the ramp into the picture and the ramp
// back out of it, and returns the colours it carried in.
//
// The ramp in lands on the tone the cell wears on the first frame of the clip,
// and the ramp home leaves from the tone it wears on the last one, because
// playback always stops at the end of a loop. A cell the picture does not
// cover on those frames uses the darkest tone: it is hidden at that moment
// anyway, and the ramp still has to start and end somewhere.
func (t *TuffBaby) dressForPicture(ch *Character, tones []byte, final ColorPair, dynamic bool) (ColorPair, error) {
	start := t.startColors(ch, final, dynamic)
	tone := func(level byte) ColorPair {
		if level == 0 {
			level = 1
		}
		pair := Fg(t.config.Tones[level-1])
		if dynamic && start.HasBg {
			// A captured cell's background is the window chrome, the selection
			// bar or the panel it came from. The picture repaints the
			// foreground; the background travels with the character rather
			// than blinking out for the length of the effect.
			pair.Bg, pair.HasBg = start.Bg, true
		}
		return pair
	}
	if err := t.buildRamp(ch, "gatherRamp", start, tone(tones[0]), 1, SyncDistance); err != nil {
		return start, err
	}
	err := t.buildRamp(ch, "settle", tone(tones[tuffFrameCount-1]), final, tuffSettleDuration, SyncNone)
	return start, err
}

// dressForExit is the same for a surplus character, which never wears a tone:
// it dims towards the darkest one on its way off screen and comes back as
// itself.
func (t *TuffBaby) dressForExit(ch *Character, final ColorPair, dynamic bool) error {
	start := t.startColors(ch, final, dynamic)
	leaving := Fg(t.config.Tones[0])
	if dynamic && start.HasBg {
		leaving.Bg, leaving.HasBg = start.Bg, true
	}
	if err := t.buildRamp(ch, "gatherRamp", start, leaving, 1, SyncDistance); err != nil {
		return err
	}
	return t.buildRamp(ch, "settle", leaving, final, tuffSettleDuration, SyncNone)
}

// startColors is what a character is wearing when the effect takes hold of it.
//
// A cell that arrived with no foreground has nothing to ramp from, which on a
// captured screen is most of it, so grey stands in. The character still
// settles on what it actually arrived with, which may be nothing at all.
func (t *TuffBaby) startColors(ch *Character, final ColorPair, dynamic bool) ColorPair {
	if !dynamic {
		return final
	}
	start := ch.Animation.InputColors
	if !start.HasFg {
		start.Fg, start.HasFg = DynamicNeutralGrey, true
	}
	return start
}

// buildRamp adds a scene that walks the character's foreground from one colour
// to another, holding whatever background it started with.
//
// A ramp synced to distance lands its last colour on the frame the character
// lands on its cell, however far it had to travel; the ones that are not
// synced run at their own rate.
func (t *TuffBaby) buildRamp(ch *Character, id string, from, to ColorPair, duration int, sync SyncMetric) error {
	scene := ch.Animation.NewScene(id, SceneOptions{
		Sync: sync, UsesInputColors: ch.UsesInputColors, Frames: tuffRampSteps + 2,
	})
	fromFg, toFg := DynamicNeutralGrey, DynamicNeutralGrey
	if from.HasFg {
		fromFg = from.Fg
	}
	if to.HasFg {
		toFg = to.Fg
	}
	ramp, err := NewGradientSteps([]Color{fromFg, toFg}, tuffRampSteps, false)
	if err != nil {
		return err
	}
	for _, color := range ramp.Spectrum {
		pair := Fg(color)
		if from.HasBg {
			pair.Bg, pair.HasBg = from.Bg, true
		}
		if err := scene.AddFrame(ch.InputSymbol, duration, VisualParams{Colors: pair}); err != nil {
			return err
		}
	}
	// The ramp can only end on a colour. A character settling back onto a
	// background it carried, or onto nothing at all, needs the real pair as a
	// last frame or it finishes wearing the stand-in grey.
	if to.HasBg || !to.HasFg {
		return scene.AddFrame(ch.InputSymbol, duration, VisualParams{Colors: to})
	}
	return nil
}

// buildFlights gives a character the path out to where the effect wants it and
// the path back to where it came from. final is what it settles on once it is
// home.
//
// A character joining the picture turns into it: it flies at the gather's
// speed along the gather's arc. A character leaving takes the straight line at
// the exit's speed and easing. See ExitSpeed.
func (t *TuffBaby) buildFlights(ch *Character, target, home Coord, final ColorPair, leaving bool) error {
	speed, ease := t.config.GatherSpeed, t.config.GatherEase
	arc := tuffArc(ch.InputCoord, target)
	if leaving && t.config.ExitSpeed > 0 {
		speed, ease, arc = t.config.ExitSpeed, t.config.ExitEase, nil
	}
	gather, err := ch.Motion.NewPath("gather", PathOptions{
		Speed: speed, Ease: ease, HasEase: true,
		Layer: tuffFlightLayer, HasLayer: true,
	})
	if err != nil {
		return err
	}
	if _, err := gather.NewWaypoint(target, arc, ""); err != nil {
		return err
	}
	back, err := ch.Motion.NewPath("home", PathOptions{
		Speed: t.config.HomeSpeed, Ease: t.config.HomeEase, HasEase: true,
		Layer: tuffFlightLayer, HasLayer: true,
	})
	if err != nil {
		return err
	}
	if _, err := back.NewWaypoint(home, tuffArc(target, home), ""); err != nil {
		return err
	}
	// A character drops back to the base layer as soon as it lands, so the
	// picture is not painted in arrival order.
	ch.RegisterEvent(PathComplete, PathCaller("gather"), SetLayer(0))
	ch.RegisterEvent(PathComplete, PathCaller("home"), SetLayer(0))
	// The settle scene can run out of frames before the flight home ends.
	// After that nothing repaints the character, so the borrowed background
	// carryFlightsOverBackgrounds put on it on its last airborne frame is the
	// last thing written and it lands wearing it. A cell that arrived with no
	// background then keeps one it never had, which breaks the whole point of
	// DynamicExistingColors. Put the settled colours back on the frame it
	// arrives, and only when the scene has stopped painting: a scene still
	// running paints the rest of its ramp itself.
	settled := final
	ch.RegisterEvent(PathComplete, PathCaller("home"), Callback(func(_ *Engine, ch *Character) {
		if ch.Animation.ActiveSceneIsComplete() {
			ch.Animation.SetAppearance(ch.InputSymbol, settled, ch.UsesInputColors)
		}
	}))
	return nil
}

// tuffArc bends a flight sideways off the straight line between two cells. The
// bend is perpendicular to the flight and proportional to its length, so the
// characters converging on the picture turn into it rather than falling into
// it, and the ones on opposite sides of the canvas curve opposite ways.
func tuffArc(from, to Coord) []Coord {
	columnDelta := to.Column - from.Column
	rowDelta := to.Row - from.Row
	if columnDelta == 0 && rowDelta == 0 {
		return nil
	}
	mid := C((from.Column+to.Column)/2, (from.Row+to.Row)/2)
	return []Coord{C(mid.Column-rowDelta/3, mid.Row+columnDelta/6)}
}

// carryFlightsOverBackgrounds paints every character still in the air over the
// background of the cell it is crossing.
//
// This effect lifts a character to tuffFlightLayer while it flies, so it passes
// in front of the ones that have already landed. Over a captured screen most
// characters carry a foreground and no background, and a fly-over at a higher
// layer therefore punches the cell it is standing on straight out: a filled bar
// or a title row shows a moving one-cell hole for as long as anything is
// crossing it.
//
// The shared carryAddedCharactersOverBackgrounds is not enough here, for two
// reasons. It only covers the characters an effect added, and here it is the
// screen's own characters doing the flying. And overInputBackground reads the
// background a cell arrived with, whoever is standing there now; that is right
// for a spark crossing a screen that stays put, and wrong for this effect,
// which empties the screen out. A character crossing a bar that has already
// flown off would pick up a blue that is no longer on the canvas.
//
// So the background is taken from the cell's own character, and only while that
// character is still standing on it. A character crossing a cell of the picture
// picks up nothing, which is correct in the other direction: the picture's
// tones are the effect's own and it is the flier that should be in front.
//
// It is a no-op outside DynamicExistingColors, where nothing carries a
// background in the first place.
func (t *TuffBaby) carryFlightsOverBackgrounds(e *Engine) {
	if e.Terminal.Config.ExistingColorHandling != DynamicExistingColors {
		return
	}
	for _, ch := range e.ActiveCharacters() {
		if !ch.IsVisible || ch.Motion.MovementIsComplete() {
			continue
		}
		visual := ch.Animation.CurrentVisual()
		if visual.Colors.HasBg || !visual.Colors.HasFg {
			continue
		}
		under := e.Terminal.CharacterAtInputCoord(ch.Motion.CurrentCoord)
		if under == nil || under == ch || !under.IsVisible ||
			under.Motion.CurrentCoord != under.InputCoord {
			continue
		}
		beneath := under.Animation.CurrentVisual().Colors
		if !beneath.HasBg {
			continue
		}
		colors := visual.Colors
		colors.Bg, colors.HasBg = beneath.Bg, true
		ch.Animation.SetAppearance(visual.Symbol, colors, ch.UsesInputColors)
	}
}

// Advance runs one frame and reports whether the effect is still going.
func (t *TuffBaby) Advance(e *Engine) bool {
	switch t.phase {
	case tuffGathering:
		e.Update()
		t.carryFlightsOverBackgrounds(e)
		if e.ActiveCount() == 0 {
			t.phase = tuffPosing
			// The picture resolves to the clip's first frame the moment the
			// last character lands, rather than a moment into playback, so the
			// cells the first frame does not cover are never seen standing.
			t.show(e, 0)
		}
		return true

	case tuffPosing:
		// The pose does not tick the engine, so it moves the clock on itself.
		// Engine.Update does that for the phases that do tick it, and no
		// Advance ever takes both paths, so the clock still counts exactly one
		// frame per frame. See the note on play.
		e.Clock.AdvanceFrame()
		if t.poseRemaining > 0 {
			t.poseRemaining--
			return true
		}
		t.phase = tuffPlaying
		t.playStart = e.Clock.Elapsed()
		return true

	case tuffPlaying:
		if !t.clipDone {
			t.play(e)
			return true
		}
		t.beginHome(e)
		fallthrough

	default:
		// The active set is read before the tick, not after it. The host reads
		// the frame once Advance has returned, so the tick that lands the last
		// character has to be the one that returns true; reporting on the set
		// after that tick would end the run a frame before the screen is back.
		if e.ActiveCount() == 0 {
			return false
		}
		e.Update()
		t.carryFlightsOverBackgrounds(e)
		return true
	}
}

// play moves the clip to wherever the clock has got to and repaints only when
// the source frame actually changes.
//
// The clip is paced by the clock rather than by a count of host frames because
// the source has a speed of its own: ten frames a second, whatever the host
// paints at. Counting host frames holds each source frame for a fixed share of
// however long a host frame lasts, so a host painting two hundred and forty
// frames a second plays the clip four times too fast and the reader sees the
// frames go by in a blur rather than sees the clip.
//
// Playback does not tick the engine either: every character has landed and
// nothing is moving, only the tones change. So this moves the clock on itself,
// one frame per Advance, exactly as Engine.Update would have. Nothing else on
// this path calls AdvanceFrame, so the clock is not counted twice.
func (t *TuffBaby) play(e *Engine) {
	e.Clock.AdvanceFrame()
	hold := 1.0 / tuffSourceFrameRate
	if t.config.SourceFrameRate > 0 {
		hold = 1.0 / t.config.SourceFrameRate
	}
	index := int((e.Clock.Elapsed() - t.playStart + tuffClockNudge) / hold)
	if index >= t.playFrames {
		// Stop on the last frame of the clip rather than wrapping to the
		// first, because that is the frame the ramp home leaves from.
		t.frame = tuffFrameCount - 1
		t.clipDone = true
		return
	}
	// A host slower than the clip lands past more than one boundary in a
	// frame. Taking the frame the clock is on rather than the next one drops
	// the ones it went by, which is what keeps the clip at its own length.
	if frame := index % tuffFrameCount; frame != t.frame {
		t.frame = frame
		t.show(e, frame)
	}
}

// show paints one source frame: every cell the frame covers wears its tone,
// and every cell it does not is taken off the screen.
//
// Hiding is what moves the silhouette. A cell outside the picture cannot be
// painted as blank instead, because over a captured screen a blank with no
// background is a hole punched through whatever the picture is standing on.
func (t *TuffBaby) show(e *Engine, frame int) {
	for i := range t.placed {
		p := &t.placed[i]
		colors, lit := t.toneColors(p, frame)
		if lit != p.visible {
			e.Terminal.SetCharacterVisibility(p.ch, lit)
			p.visible = lit
		}
		if !lit {
			continue
		}
		tuffSetTone(&p.ch.Animation, p.ch.InputSymbol, colors,
			p.tones[frame] >= tuffBoldFrom, p.ch.UsesInputColors)
	}
}

// tuffSetTone is Animation.SetAppearance with the weight as well as the
// colour.
//
// A glyph fills about a third of its cell and how much it fills varies from
// glyph to glyph, so over a screen of arbitrary text that variation is louder
// than a five step ramp of colour: the picture comes out as noise with a
// gradient behind it. Bold on the light end roughly doubles the ink in the
// cells that are meant to read as lit, and puts the tonal order back.
//
// It lives here rather than as a method on Animation because animation.go is
// a port and this is not: nothing upstream needs a weight set outside a scene,
// and the deviation belongs in the effect that wanted it. Everything it
// touches is package-internal, and it honours AlwaysExistingColors the same
// way SetAppearance does.
func tuffSetTone(a *Animation, symbol string, colors ColorPair, bold, usesInputColors bool) {
	if a.ExistingColorHandling == AlwaysExistingColors && usesInputColors {
		colors, bold = a.InputColors, a.InputBold
	}
	a.currentVisual = a.visuals.get(symbol, VisualParams{Bold: bold, Colors: colors})
}

// beginHome sends everything back where it came from.
func (t *TuffBaby) beginHome(e *Engine) {
	t.phase = tuffGoingHome
	// Every cell of the picture flies home, including the ones the last frame
	// of the clip had hidden. They came from the screen and they go back to it.
	for i := range t.placed {
		if !t.placed[i].visible {
			e.Terminal.SetCharacterVisibility(t.placed[i].ch, true)
			t.placed[i].visible = true
		}
	}
	for _, ch := range e.Terminal.CollectCharacters(CharacterFilter{Input: true, Added: true}) {
		e.ActivateScene(ch, "settle")
		e.Activate(ch)
		e.ActivatePath(ch, "home")
	}
	// An appended character has no home to go back to, so it leaves the way it
	// arrived and drops out of the visible set the moment it gets there.
	//
	// Its home is one cell past a canvas edge and the painter skips a
	// coordinate off the canvas, so this is not what keeps it off the screen.
	// It is what keeps the painter from walking it once it is gone, and it is
	// the guard if anything ever gives an appended character a home that is on
	// the canvas.
	for _, ch := range t.added {
		ch.RegisterEvent(PathComplete, PathCaller("home"), Callback(func(e *Engine, ch *Character) {
			e.Terminal.SetCharacterVisibility(ch, false)
		}))
	}
}
