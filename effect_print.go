package tuiffects

// print, ported from ttfx src/effects/print_effect.rs, which ports
// TerminalTextEffects effects/effect_print.py by ChrisBuilds.

import "fmt"

func init() {
	Register(Descriptor{
		Name:        "print",
		Description: "Lines are typed out one at a time, and a print head runs back to the margin between them",
		New:         func() Effect { return NewPrint(DefaultPrintConfig()) },
		// The effect types every row of the canvas, not only the rows the
		// input happens to land on, and it finds them by querying the inner
		// and outer fill populations. Upstream always makes fill characters,
		// so no ttfx effect declares this.
		//
		// Without it the query returns only the rows holding input. The text
		// then scrolls up by the number of those rows instead of by the
		// height of the canvas, so it finishes sitting at the bottom rather
		// than where it started, and the rows above and below it never move.
		NeedsFillCharacters: true,
	})
}

// PrintConfig tunes the print effect.
type PrintConfig struct {
	// PrintHeadReturnSpeed is how fast the head travels back to the start of
	// the next line. Raise it to shorten the pause between lines.
	PrintHeadReturnSpeed float64
	// PrintSpeed is how many characters are typed per frame.
	PrintSpeed int
	// PrintHeadEasing shapes the head's travel during a carriage return.
	PrintHeadEasing Easing
	// FinalGradientStops colour the typed text. They are ignored when the
	// engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultPrintConfig is upstream's default print.
func DefaultPrintConfig() PrintConfig {
	return PrintConfig{
		PrintHeadReturnSpeed: 1.5,
		PrintSpeed:           2,
		PrintHeadEasing:      InOutQuad,
		FinalGradientStops: []Color{
			MustParseColor("02b8bd"), MustParseColor("c1f0e3"), MustParseColor("00ffa0"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Diagonal,
	}
}

// printCarriageReturnPath is the id the head's return path is rebuilt under
// every line. The id is reused rather than auto-allocated because the event
// that hides the head at the end of the sweep is keyed to it.
const printCarriageReturnPath = "carriage_return_path"

// printHeadColor is what a character wears the instant the head strikes it,
// before it ramps to its final colour. It is fixed upstream rather than
// configurable.
var printHeadColor = MustParseColor("ffffff")

// printFallbackColor is what a character gets when it sits outside the text
// block and the final gradient mapping has no entry for it.
var printFallbackColor = MustParseColor("ffffff")

// printRow is one line of the canvas, split into what the head has already
// struck and what it has not. Ported from PrintIterator.Row.
type printRow struct {
	untyped []*Character
	typed   []*Character
}

// moveUp lifts every struck character one row. It is called on each finished
// line as the next one starts, which is what scrolls the page.
func (r *printRow) moveUp() {
	for _, ch := range r.typed {
		current := ch.Motion.CurrentCoord
		ch.Motion.SetCoordinate(C(current.Column, current.Row+1))
	}
}

// typeChar takes the next character off the front of the line, or reports nil
// when the line is finished.
func (r *printRow) typeChar() *Character {
	if len(r.untyped) == 0 {
		return nil
	}
	next := r.untyped[0]
	r.untyped = r.untyped[1:]
	r.typed = append(r.typed, next)
	return next
}

// Print types the canvas out one line at a time on the bottom row, lifting the
// finished lines above it as it goes, so the text arrives the way a teletype
// would deliver it. Between lines a block runs back along the bottom row to
// the column the next line starts in.
//
// This effect assembles the screen rather than passing over it, so every
// character stays hidden until the head strikes it. That holds under every
// colour policy, including DynamicExistingColors: showing the picture up front
// would leave the head nothing to type.
type Print struct {
	config PrintConfig

	// pendingRows are the lines still to type, top of the input first, and
	// processedRows are the ones already typed, which scroll up together.
	pendingRows   []printRow
	processedRows []printRow
	currentRow    printRow

	typingHead  *Character
	finalColors map[*Character]ColorPair

	typing     bool
	lastColumn int
	// headEventHung records that the head's hide-on-arrival handler is in
	// place. Upstream re-registers it every line and swallows the duplicate
	// error Python raises; nothing here rejects a duplicate, so registering
	// once is what keeps the handler list from growing per line.
	headEventHung bool
}

// NewPrint builds the effect.
func NewPrint(config PrintConfig) *Print {
	return &Print{config: config}
}

// Build parks every character on the bottom row under its own column, gives it
// the scene that fades it in from the head's strike, and splits the canvas into
// the lines the head will type.
func (p *Print) Build(e *Engine) error {
	if p.config.PrintHeadReturnSpeed <= 0 {
		return fmt.Errorf("print: the head return speed must be above 0, got %v", p.config.PrintHeadReturnSpeed)
	}
	if p.config.PrintSpeed < 1 {
		return fmt.Errorf("print: the print speed must be at least 1, got %d", p.config.PrintSpeed)
	}

	// PrintIterator.__init__: the head is a character of its own, added
	// before the lines are built so it paints over them.
	p.typingHead = e.Terminal.AddCharacter("█", C(1, 1))

	gradient, err := NewGradient(p.config.FinalGradientStops, p.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		p.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	filter := CharacterFilter{Input: true, InnerFill: true, OuterFill: true}
	characters := e.Terminal.GetCharacters(e.Rng, filter, SortTopToBottomLeftToRight)
	p.finalColors = make(map[*Character]ColorPair, len(characters))
	for _, ch := range characters {
		if dynamic {
			// Both halves are carried, so a cell that arrived with a
			// background keeps it. A cell that arrived with neither reads as
			// an empty pair here and takes the colourless branch in makeRow.
			p.finalColors[ch] = ch.Animation.InputColors
		} else {
			p.finalColors[ch] = Fg(mapping.At(ch.InputCoord, printFallbackColor))
		}
	}

	for _, group := range e.Terminal.GetCharactersGrouped(filter, GroupRowTopToBottom) {
		row, err := p.makeRow(e, group, dynamic)
		if err != nil {
			return err
		}
		p.pendingRows = append(p.pendingRows, row)
	}
	if len(p.pendingRows) == 0 {
		return fmt.Errorf("print: the canvas has no characters to type")
	}
	p.currentRow = p.pendingRows[0]
	p.pendingRows = p.pendingRows[1:]
	p.typing = true
	p.lastColumn = 0
	return nil
}

// makeRow prepares one line: it drops the part of the line the head will never
// reach, parks the rest on the bottom row, and gives each character the scene
// that takes it from a solid block down to its own glyph.
//
// Ported from PrintIterator.Row.__init__.
func (p *Print) makeRow(e *Engine, characters []*Character, dynamic bool) (printRow, error) {
	allSpaces := true
	for _, ch := range characters {
		// On a captured screen a space that carries a background is not a
		// blank: it is a filled bar, a divider, a piece of window chrome.
		// Collapsing such a row would leave it out of the finished picture
		// for good, because nothing after this ever makes those cells
		// visible again. Upstream animates piped text, where a row of spaces
		// really is empty, so the default path is left exactly as it was.
		if ch.InputSymbol != " " || (dynamic && ch.Animation.InputColors.HasBg) {
			allSpaces = false
			break
		}
	}
	if allSpaces {
		// A blank line is typed as a single character, so the head does not
		// walk the width of the canvas to print nothing.
		characters = characters[:1]
	} else {
		// A fill character is always a space, so a line holding any non-space
		// holds a non-fill character and this extent always exists.
		rightExtent := 0
		for _, ch := range characters {
			if !ch.IsFill && ch.InputCoord.Column > rightExtent {
				rightExtent = ch.InputCoord.Column
			}
		}
		kept := make([]*Character, 0, len(characters))
		for _, ch := range characters {
			if ch.InputCoord.Column <= rightExtent {
				kept = append(kept, ch)
			}
		}
		characters = kept
	}

	untyped := make([]*Character, 0, len(characters))
	for _, ch := range characters {
		ch.Motion.SetCoordinate(C(ch.InputCoord.Column, 1))
		typed := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          6,
		})
		final := p.finalColors[ch]

		switch {
		case dynamic && (final.HasFg || final.HasBg):
			var fgRamp, bgRamp *Gradient
			var err error
			if final.HasFg {
				if fgRamp, err = NewGradientSteps([]Color{printHeadColor, final.Fg}, 5, false); err != nil {
					return printRow{}, err
				}
			}
			if final.HasBg {
				if bgRamp, err = NewGradientSteps([]Color{printHeadColor, final.Bg}, 5, false); err != nil {
					return printRow{}, err
				}
			}
			if err := typed.ApplyGradientToSymbols(
				[]string{"█", "▓", "▒", "░", ch.InputSymbol}, 3, fgRamp, bgRamp); err != nil {
				return printRow{}, err
			}
		case dynamic:
			// The cell carried no colour of its own. It fades from the head's
			// white down to nothing, so the last frame has to be added by
			// hand: a gradient always paints a colour and this one must not.
			head, err := NewGradientSteps([]Color{printHeadColor, printHeadColor}, 4, false)
			if err != nil {
				return printRow{}, err
			}
			if err := typed.ApplyGradientToSymbols([]string{"█", "▓", "▒", "░"}, 3, head, nil); err != nil {
				return printRow{}, err
			}
			if err := typed.AddFrame(ch.InputSymbol, 3, VisualParams{Colors: ColorPair{}}); err != nil {
				return printRow{}, err
			}
		default:
			ramp, err := NewGradientSteps([]Color{printHeadColor, final.Fg}, 5, false)
			if err != nil {
				return printRow{}, err
			}
			if err := typed.ApplyGradientToSymbols(
				[]string{"█", "▓", "▒", "░", ch.InputSymbol}, 3, ramp, nil); err != nil {
				return printRow{}, err
			}
		}

		e.ActivateScene(ch, typed.ID)
		untyped = append(untyped, ch)
	}
	return printRow{untyped: untyped}, nil
}

// Advance types the next few characters, or runs the head back to the margin,
// and reports whether the effect is still going.
func (p *Print) Advance(e *Engine) bool {
	if e.ActiveCount() == 0 && !p.typing {
		return false
	}
	if !p.typingHead.Motion.MovementIsComplete() {
		// The head is performing a carriage return. Nothing is typed while it
		// travels.
	} else if len(p.currentRow.untyped) > 0 {
		count := min(len(p.currentRow.untyped), p.config.PrintSpeed)
		for i := 0; i < count; i++ {
			next := p.currentRow.typeChar()
			if next == nil {
				break
			}
			e.Terminal.SetCharacterVisibility(next, true)
			e.Activate(next)
			p.lastColumn = next.InputCoord.Column
		}
	} else {
		p.finishLine(e)
	}
	e.Update()
	return true
}

// finishLine files the line just typed, scrolls everything typed so far up by
// one row, and sends the head back to the start of the next line.
func (p *Print) finishLine(e *Engine) {
	p.processedRows = append(p.processedRows, p.currentRow)
	p.currentRow = printRow{}
	if len(p.pendingRows) == 0 {
		p.typing = false
		return
	}
	for i := range p.processedRows {
		p.processedRows[i].moveUp()
	}
	p.currentRow = p.pendingRows[0]
	p.pendingRows = p.pendingRows[1:]
	p.trimCurrentRow(e)

	p.typingHead.Motion.SetCoordinate(C(p.lastColumn, 1))
	e.Terminal.SetCharacterVisibility(p.typingHead, true)
	target := p.currentRow.untyped[0].InputCoord.Column
	if err := p.startCarriageReturn(e, target); err != nil {
		// Unreachable. Build rejects a return speed at or below zero, which
		// is NewPath's only failure, and ClearPaths has just emptied the path
		// table, which is NewWaypoint's. If it ever did happen, the head has
		// no sweep to show, so it is put away and the next line types on time.
		e.Terminal.SetCharacterVisibility(p.typingHead, false)
		return
	}
	if !p.headEventHung {
		p.headEventHung = true
		p.typingHead.RegisterEvent(PathComplete, PathCaller(printCarriageReturnPath),
			Callback(func(e *Engine, ch *Character) { e.Terminal.SetCharacterVisibility(ch, false) }))
	}
	e.Activate(p.typingHead)
}

// trimCurrentRow drops the leading blank cells of the next line, so the head
// stops at the line's first real character rather than at the margin, and
// drops anything past the right edge of the text block.
//
// A line of nothing but fill characters is left alone at both ends: there is
// no first real character to stop at.
func (p *Print) trimCurrentRow(e *Engine) {
	previous := &p.processedRows[len(p.processedRows)-1]
	if printAllFill(previous.typed) || printAllFill(p.currentRow.untyped) {
		return
	}
	leftExtent := 0
	found := false
	for _, ch := range p.currentRow.untyped {
		if ch.IsFill {
			continue
		}
		if !found || ch.InputCoord.Column < leftExtent {
			leftExtent, found = ch.InputCoord.Column, true
		}
	}
	textRight := e.Terminal.Canvas.TextRight
	kept := p.currentRow.untyped[:0]
	for _, ch := range p.currentRow.untyped {
		if leftExtent <= ch.InputCoord.Column && ch.InputCoord.Column <= textRight {
			kept = append(kept, ch)
		}
	}
	p.currentRow.untyped = kept
}

// startCarriageReturn rebuilds the head's return path and sets it running.
func (p *Print) startCarriageReturn(e *Engine, targetColumn int) error {
	p.typingHead.Motion.ClearPaths()
	path, err := p.typingHead.Motion.NewPath(printCarriageReturnPath, PathOptions{
		Speed:   p.config.PrintHeadReturnSpeed,
		Ease:    p.config.PrintHeadEasing,
		HasEase: true,
	})
	if err != nil {
		return err
	}
	if _, err := path.NewWaypoint(C(targetColumn, 1), nil, ""); err != nil {
		return err
	}
	e.ActivatePath(p.typingHead, printCarriageReturnPath)
	return nil
}

// printAllFill reports whether every character in a line is one the engine
// invented to pad the canvas.
func printAllFill(characters []*Character) bool {
	for _, ch := range characters {
		if !ch.IsFill {
			return false
		}
	}
	return true
}
