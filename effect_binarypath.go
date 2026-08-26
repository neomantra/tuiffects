package tuiffects

// binarypath, ported from ttfx src/effects/binarypath.rs, which ports
// TerminalTextEffects effects/effect_binarypath.py by ChrisBuilds.

import (
	"fmt"
	"unicode/utf8"
)

func init() {
	Register(Descriptor{
		Name:        "binarypath",
		Description: "Each character breaks into the binary digits of its code point, which travel the screen and collapse back into it",
		New:         func() Effect { return NewBinaryPath(DefaultBinaryPathConfig()) },
	})
}

// BinaryPathConfig tunes the binarypath effect.
type BinaryPathConfig struct {
	// FinalGradientStops colour the text once it has been rebuilt. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
	// BinaryColors are the colours the travelling digits wear. Each digit
	// picks one at random.
	BinaryColors []Color
	// MovementSpeed is how fast a digit travels along its path.
	MovementSpeed float64
	// ActiveBinaryGroups is how many groups of digits may travel at once, as
	// a fraction of the total number of groups. Lower it to do less work per
	// frame.
	ActiveBinaryGroups float64
}

// DefaultBinaryPathConfig is upstream's default binarypath.
func DefaultBinaryPathConfig() BinaryPathConfig {
	return BinaryPathConfig{
		FinalGradientStops: []Color{
			MustParseColor("00d500"), MustParseColor("007500"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Radial,
		BinaryColors: []Color{
			MustParseColor("044E29"), MustParseColor("157e38"),
			MustParseColor("45bf55"), MustParseColor("95ed87"),
		},
		MovementSpeed:      1.0,
		ActiveBinaryGroups: 0.08,
	}
}

// binaryRepresentation is one input character and the digits that carry it,
// ttfx BinaryRepresentation, upstream _BinaryRepresentation.
type binaryRepresentation struct {
	character *Character
	// binaryCharacters are every digit of this character's code point.
	// pendingBinaryCharacters are the ones not yet released, and shrinks by
	// one a frame so the group leaves as a stream rather than a block.
	binaryCharacters        []*Character
	pendingBinaryCharacters []*Character
	inputCoord              Coord
	isActive                bool
}

// travelComplete reports whether every digit of the group has reached the cell
// its character belongs in.
func (b *binaryRepresentation) travelComplete() bool {
	for _, digit := range b.binaryCharacters {
		if digit.Motion.CurrentCoord != b.inputCoord {
			return false
		}
	}
	return true
}

// binaryPathPhase is which half of the run the effect is in.
type binaryPathPhase int

const (
	// binaryPathTravel sends groups of digits around the canvas and collapses
	// each one back into its character.
	binaryPathTravel binaryPathPhase = iota
	// binaryPathWipe brightens the rebuilt text a diagonal at a time.
	binaryPathWipe
)

// binaryPathOrientation is the axis the last leg of a path ran along. It is
// upstream's typing.Literal["col", "row"].
type binaryPathOrientation int

const (
	binaryPathColumn binaryPathOrientation = iota
	binaryPathRow
)

// BinaryPath breaks every character into the binary digits of its code point,
// sends those digits on a right-angled path around the canvas, and collapses
// them back into the character when they arrive. A diagonal wipe then
// brightens the rebuilt text.
//
// This effect assembles the screen rather than passing over it, so every
// character stays hidden until its own digits have arrived. That holds under
// every colour policy, including DynamicExistingColors: showing the picture up
// front would leave the digits nothing to rebuild.
type BinaryPath struct {
	config BinaryPathConfig

	// pendingReps are the groups not yet travelling and activeReps the ones
	// that are. maxActiveGroups caps the second.
	pendingReps     []*binaryRepresentation
	activeReps      []*binaryRepresentation
	maxActiveGroups int

	// finalColors is the colour each character settles on, resolved in Build
	// before any digit is made.
	finalColors map[*Character]ColorPair

	// finalWipeChars are the diagonals of the closing wipe, in order.
	finalWipeChars [][]*Character

	phase             binaryPathPhase
	complete          bool
	lastFrameProvided bool
}

// NewBinaryPath builds the effect.
func NewBinaryPath(config BinaryPathConfig) *BinaryPath {
	return &BinaryPath{
		phase:       binaryPathTravel,
		config:      config,
		finalColors: make(map[*Character]ColorPair),
	}
}

// Build resolves every character's final colour, makes the digits that carry
// it and the path each digit follows, and gives the character the two scenes
// it wears once the digits arrive.
func (b *BinaryPath) Build(e *Engine) error {
	// Upstream computes the wipe order in __init__, before the rest of the
	// build. It reads the same either way, because nothing here moves an
	// input character out of its input coordinate.
	b.finalWipeChars = e.Terminal.GetCharactersGrouped(InputOnly(), GroupDiagonalTopRightToBottomLeft)

	finalGradient, err := NewGradient(b.config.FinalGradientStops, b.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		b.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)
	for _, ch := range characters {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			// The whole pair, not just the foreground. A cell that arrived
			// with a background keeps it, so a selection bar or a filled
			// panel is rebuilt as itself rather than as bare text.
			final = ch.Animation.InputColors
		}
		b.finalColors[ch] = final
	}

	white := MustParseColor("ffffff")
	for _, ch := range characters {
		if err := b.buildRepresentation(e, ch); err != nil {
			return err
		}
	}

	for _, ch := range characters {
		final := b.finalColors[ch]
		var dimFg, dimBg Color
		if final.HasFg {
			dimFg = AdjustColorBrightness(final.Fg, 0.5)
		}
		if final.HasBg {
			dimBg = AdjustColorBrightness(final.Bg, 0.5)
		}

		// The collapse: the character flashes white and falls back to half
		// its final brightness, as if the digits had just landed on it.
		var collapseFg, collapseBg *Gradient
		if final.HasFg {
			if collapseFg, err = NewGradientSteps([]Color{white, dimFg}, 7, false); err != nil {
				return err
			}
		}
		if final.HasBg {
			if collapseBg, err = NewGradientSteps([]Color{white, dimBg}, 7, false); err != nil {
				return err
			}
		}
		collapse := ch.Animation.NewScene("collapse_scn", SceneOptions{
			Ease:            InQuad,
			HasEase:         true,
			UsesInputColors: ch.UsesInputColors,
			Frames:          binaryPathGradientFrames(collapseFg, collapseBg),
		})
		if collapseFg == nil && collapseBg == nil {
			// The character arrived with no colours of its own, so there is
			// nothing to dim towards and one plain frame is the whole scene.
			// Only the dynamic branch can reach this.
			if err := collapse.AddFrame(ch.InputSymbol, 3, VisualParams{}); err != nil {
				return err
			}
		} else if err := collapse.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, 3, collapseFg, collapseBg); err != nil {
			return err
		}

		// The brighten: the closing wipe takes the character from half
		// brightness up to its final colour.
		var brightenFg, brightenBg *Gradient
		if final.HasFg {
			if brightenFg, err = NewGradientSteps([]Color{dimFg, final.Fg}, 10, false); err != nil {
				return err
			}
		}
		if final.HasBg {
			if brightenBg, err = NewGradientSteps([]Color{dimBg, final.Bg}, 10, false); err != nil {
				return err
			}
		}
		brighten := ch.Animation.NewScene("brighten_scn", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          binaryPathGradientFrames(brightenFg, brightenBg),
		})
		if brightenFg == nil && brightenBg == nil {
			if err := brighten.AddFrame(ch.InputSymbol, 2, VisualParams{}); err != nil {
				return err
			}
		} else if err := brighten.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, 2, brightenFg, brightenBg); err != nil {
			return err
		}
	}

	b.maxActiveGroups = max(1, int(b.config.ActiveBinaryGroups*float64(len(b.pendingReps))))
	return nil
}

// binaryPathGradientFrames is how many frames a scene built from these two gradients
// ends up with, so the scene can size its slices once.
func binaryPathGradientFrames(fg, bg *Gradient) int {
	frames := 0
	if fg != nil {
		frames = len(fg.Spectrum)
	}
	if bg != nil && len(bg.Spectrum) > frames {
		frames = len(bg.Spectrum)
	}
	return frames
}

// buildRepresentation makes one character's digits, plots the path they all
// follow, and starts them on it.
func (b *BinaryPath) buildRepresentation(e *Engine, ch *Character) error {
	symbol := ch.Animation.CurrentVisual().Symbol
	codePoint, _ := utf8.DecodeRuneInString(symbol)
	if codePoint == utf8.RuneError {
		codePoint = 0
	}
	// Eight digits is a minimum width, not a maximum: a code point above 255
	// gets as many digits as it needs, exactly as upstream's format(ord, "08b")
	// does.
	digits := fmt.Sprintf("%08b", codePoint)

	rep := &binaryRepresentation{character: ch, inputCoord: ch.InputCoord}
	for _, digit := range digits {
		added := e.Terminal.AddCharacter(string(digit), C(0, 0))
		rep.binaryCharacters = append(rep.binaryCharacters, added)
		rep.pendingBinaryCharacters = append(rep.pendingBinaryCharacters, added)
	}
	b.pendingReps = append(b.pendingReps, rep)

	pathCoords := b.plotPath(e, ch.InputCoord)
	for _, digit := range rep.binaryCharacters {
		digit.Motion.SetCoordinate(pathCoords[0])
		path, err := digit.Motion.NewPath("", PathOptions{Speed: b.config.MovementSpeed})
		if err != nil {
			return err
		}
		for _, coord := range pathCoords {
			if _, err := path.NewWaypoint(coord, nil, ""); err != nil {
				return err
			}
		}
		e.ActivatePath(digit, path.ID)
		// Above the text, so a digit passing over a rebuilt character hides
		// it rather than disappearing behind it.
		digit.Layer = 1

		color := *Choice(e.Rng, b.config.BinaryColors)
		scene := digit.Animation.NewScene("", SceneOptions{
			UsesInputColors: digit.UsesInputColors,
			Frames:          1,
		})
		if err := scene.AddFrame(digit.Animation.CurrentVisual().Symbol, 1, VisualParams{
			Colors: Fg(color),
		}); err != nil {
			return err
		}
		e.ActivateScene(digit, scene.ID)
	}
	return nil
}

// plotPath walks from a random point off the edge of the canvas to a target
// cell in right-angled legs, and returns the waypoints of that walk. Long legs
// run along the row and short ones along the column, which is what gives the
// digits their circuit-board look.
func (b *BinaryPath) plotPath(e *Engine, inputCoord Coord) []Coord {
	canvas := e.Terminal.Canvas
	start := canvas.RandomCoord(e.Rng, true, false)
	pathCoords := []Coord{start}
	// The names are upstream's and read backwards: the leg taken while the
	// orientation is Column runs along the row, and the other way about.
	lastOrientation := *Choice(e.Rng, []binaryPathOrientation{binaryPathColumn, binaryPathRow})
	nextCoord := start

	for pathCoords[len(pathCoords)-1] != inputCoord {
		lastCoord := pathCoords[len(pathCoords)-1]
		columnDirection := 0
		switch {
		case lastCoord.Column > inputCoord.Column:
			columnDirection = -1
		case lastCoord.Column < inputCoord.Column:
			columnDirection = 1
		}
		rowDirection := 0
		switch {
		case lastCoord.Row > inputCoord.Row:
			rowDirection = -1
		case lastCoord.Row < inputCoord.Row:
			rowDirection = 1
		}
		maxColumnDistance := abs(lastCoord.Column - inputCoord.Column)
		maxRowDistance := abs(lastCoord.Row - inputCoord.Row)

		switch {
		case lastOrientation == binaryPathColumn && maxRowDistance > 0:
			// int() truncation upstream, so a canvas under 50 columns wide
			// always gets the floor of 10.
			limit := min(maxRowDistance, max(10, int(float64(canvas.Right)*0.2)))
			nextCoord = C(lastCoord.Column, lastCoord.Row+e.Rng.IntBetween(1, limit)*rowDirection)
			lastOrientation = binaryPathRow
		case lastOrientation == binaryPathRow && maxColumnDistance > 0:
			nextCoord = C(
				lastCoord.Column+e.Rng.IntBetween(1, min(maxColumnDistance, 4))*columnDirection,
				lastCoord.Row)
			lastOrientation = binaryPathColumn
		default:
			nextCoord = inputCoord
		}
		pathCoords = append(pathCoords, nextCoord)
	}

	// Upstream appends the last leg a second time and then the destination, so
	// the digits hold on the target cell for a step before the collapse. Both
	// pushes are deliberate.
	pathCoords = append(pathCoords, nextCoord)
	pathCoords = append(pathCoords, inputCoord)
	return pathCoords
}

// Advance releases another digit from each travelling group, collapses the
// groups that have arrived, and runs one frame. It reports whether the effect
// is still going.
func (b *BinaryPath) Advance(e *Engine) bool {
	if b.complete && e.ActiveCount() == 0 {
		if !b.lastFrameProvided {
			// Upstream hands back one more frame after the run is over, so
			// the settled picture is seen rather than inferred.
			b.lastFrameProvided = true
			return true
		}
		return false
	}

	if b.phase == binaryPathTravel {
		b.startGroups(e)
		b.stepGroups(e)
		if e.ActiveCount() == 0 {
			b.phase = binaryPathWipe
		}
	}

	if b.phase == binaryPathWipe {
		// Two diagonals a frame, which is upstream's wipe speed.
		for i := 0; i < 2; i++ {
			if len(b.finalWipeChars) == 0 {
				b.complete = true
				continue
			}
			next := b.finalWipeChars[0]
			b.finalWipeChars = b.finalWipeChars[1:]
			for _, ch := range next {
				e.ActivateScene(ch, "brighten_scn")
				e.Terminal.SetCharacterVisibility(ch, true)
				e.Activate(ch)
			}
		}
	}

	e.Update()
	// A digit carries a foreground and no background and sits a layer above
	// the text, so without this it punches a hole straight through whatever it
	// is flying across: a status bar sparkles with black gaps for the length
	// of the run, because roughly a ninth of the filled cells are under a
	// digit at any moment and the holes move every frame.
	carryAddedCharactersOverBackgrounds(e)
	return true
}

// startGroups tops the travelling groups back up to the cap, taking each new
// one from a random place in the queue.
func (b *BinaryPath) startGroups(e *Engine) {
	for len(b.activeReps) < b.maxActiveGroups && len(b.pendingReps) > 0 {
		index := e.Rng.IntBelow(0, len(b.pendingReps))
		next := b.pendingReps[index]
		b.pendingReps = append(b.pendingReps[:index], b.pendingReps[index+1:]...)
		next.isActive = true
		b.activeReps = append(b.activeReps, next)
	}
}

// stepGroups releases one more digit from every travelling group, and
// collapses the groups whose digits have all arrived.
func (b *BinaryPath) stepGroups(e *Engine) {
	kept := b.activeReps[:0]
	for _, rep := range b.activeReps {
		switch {
		case len(rep.pendingBinaryCharacters) > 0:
			next := rep.pendingBinaryCharacters[0]
			rep.pendingBinaryCharacters = rep.pendingBinaryCharacters[1:]
			e.Activate(next)
			e.Terminal.SetCharacterVisibility(next, true)
		case rep.travelComplete():
			for _, digit := range rep.binaryCharacters {
				e.Terminal.SetCharacterVisibility(digit, false)
			}
			rep.isActive = false
			e.Terminal.SetCharacterVisibility(rep.character, true)
			e.ActivateScene(rep.character, "collapse_scn")
			e.Activate(rep.character)
		}
		if rep.isActive {
			kept = append(kept, rep)
		}
	}
	b.activeReps = kept
}
