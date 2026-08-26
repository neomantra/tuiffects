package tuiffects

// overflow, ported from ttfx src/effects/overflow.rs, which ports
// TerminalTextEffects effects/effect_overflow.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "overflow",
		Description: "Rows of the text scroll up the screen out of order, then the real rows scroll in behind them",
		New:         func() Effect { return NewOverflow(DefaultOverflowConfig()) },
		// The rows that scroll in last are the whole canvas, not just the
		// text, so the bottom row of the picture lands on row 1. Without fill
		// characters only the text rows exist, the scroll stops short of the
		// bottom, and the picture settles in the wrong place.
		NeedsFillCharacters: true,
	})
}

// OverflowConfig tunes the overflow effect.
type OverflowConfig struct {
	// OverflowGradientStops colour the rows while they are scrolling past.
	// The gradient runs up the canvas, so a row changes colour as it climbs.
	OverflowGradientStops []Color
	// OverflowCyclesLow and OverflowCyclesHigh bound how many times the text
	// is scrambled and scrolled past before the real rows arrive. Setting
	// OverflowCyclesHigh to zero skips the scramble entirely.
	OverflowCyclesLow  int
	OverflowCyclesHigh int
	// OverflowSpeed is the most rows that can be released in one frame.
	OverflowSpeed int
	// FinalGradientStops colour the text once it lands. They are ignored when
	// the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultOverflowConfig is upstream's default overflow.
func DefaultOverflowConfig() OverflowConfig {
	return OverflowConfig{
		OverflowGradientStops: []Color{
			MustParseColor("f2ebc0"), MustParseColor("8dbfb3"), MustParseColor("f2ebc0"),
		},
		OverflowCyclesLow:  2,
		OverflowCyclesHigh: 4,
		OverflowSpeed:      3,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// overflowRow is one row of characters travelling up the canvas together.
//
// ttfx passes the engine into every one of these because its characters live
// in an arena and it needs the arena to reach them. Here a character is a
// pointer and only its own motion and animation are touched, so none of the
// three needs the engine.
//
// A row is never empty. Build drops any that would be, because Advance reads
// the first character of a row to find out what row it has climbed to.
type overflowRow struct {
	characters []*Character
	// final marks the row as part of the real picture rather than a copy
	// scrolling past. A final row keeps the colour Build gave it; a copy is
	// recoloured from the overflow gradient every time it climbs a row.
	final bool
}

// moveUp lifts every character in the row by one row.
func (r *overflowRow) moveUp() {
	for _, ch := range r.characters {
		current := ch.Motion.CurrentCoord
		ch.Motion.SetCoordinate(C(current.Column, current.Row+1))
	}
}

// setup parks the row on row 0, one below the bottom of the canvas, with each
// character back in the column it came from.
func (r *overflowRow) setup() {
	for _, ch := range r.characters {
		ch.Motion.SetCoordinate(C(ch.InputCoord.Column, 0))
	}
}

// setColor repaints the whole row. ttfx takes a foreground and a background
// here and every caller passes a foreground alone, so this takes the pair it
// would have built.
//
// Only copies are repainted, and a foreground alone is right for them even
// under DynamicExistingColors: a copy is scrap that scrolls past and off, the
// canvas underneath it is still empty, and carrying the captured backgrounds
// through would flash every panel of the screen on its way up.
func (r *overflowRow) setColor(colors ColorPair) {
	for _, ch := range r.characters {
		ch.Animation.SetAppearance(ch.InputSymbol, colors, ch.UsesInputColors)
	}
}

// Overflow scrolls the text up the canvas in the wrong order, over and over,
// and then scrolls the real rows in behind it so the picture arrives from the
// bottom edge.
//
// This effect assembles the screen rather than passing over it, so every
// character stays hidden until the row carrying it enters from the bottom.
// That holds under every colour policy, DynamicExistingColors included:
// showing the picture up front would leave the scroll nothing to deliver, and
// the copies would scrape across a screen that was already finished.
type Overflow struct {
	config OverflowConfig

	// pendingRows are the rows still to enter, in order: every scrambled copy
	// first, then the real rows from the top of the canvas down. activeRows
	// are the ones on screen now.
	pendingRows []*overflowRow
	activeRows  []*overflowRow

	// delay counts frames until the next release.
	delay int

	overflowGradient *Gradient
}

// NewOverflow builds the effect.
func NewOverflow(config OverflowConfig) *Overflow {
	return &Overflow{config: config}
}

// Build makes the scrambled copies of the text, queues them ahead of the real
// rows, and settles every real character on the colour it will end on.
func (o *Overflow) Build(e *Engine) error {
	finalGradient, err := NewGradient(o.config.FinalGradientStops, o.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		o.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	// ttfx falls back to black rather than to the first spectrum colour for a
	// coordinate the mapping misses. Only an outer fill character can land
	// outside the text rectangle and those are spaces, so it is a colour
	// nothing ever shows.
	fallback := MustParseColor("000000")
	fills := CharacterFilter{Input: true, InnerFill: true, OuterFill: true}
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	// The scrambled copies. Each cycle shuffles the rows and queues a copy of
	// every one of them, so the same text goes past in a different order each
	// time round.
	if o.config.OverflowCyclesHigh > 0 {
		rows := e.Terminal.GetCharactersGrouped(InputOnly(), GroupRowTopToBottom)
		cycles := e.Rng.IntBetween(o.config.OverflowCyclesLow, o.config.OverflowCyclesHigh)
		for cycle := 0; cycle < cycles; cycle++ {
			Shuffle(e.Rng, rows)
			for _, row := range rows {
				copied := make([]*Character, 0, len(row))
				for _, ch := range row {
					// A copy is a new character at the same coordinate wearing
					// the same symbol and the same input colours. ttfx does not
					// carry the input's bold flag across, so a bold cell's copy
					// is not bold under AlwaysExistingColors; that is left as
					// it is.
					ghost := e.Terminal.AddCharacter(ch.InputSymbol, ch.InputCoord)
					ghost.UsesInputColors = true
					ghost.Animation.InputColors = ch.Animation.InputColors
					copied = append(copied, ghost)
				}
				if len(copied) == 0 {
					continue
				}
				o.pendingRows = append(o.pendingRows, &overflowRow{characters: copied})
			}
		}
	}

	// Then the real rows, top of the canvas first, so the bottom row is the
	// last one in and lands on row 1.
	for _, row := range e.Terminal.GetCharactersGrouped(fills, GroupRowTopToBottom) {
		if len(row) == 0 {
			continue
		}
		for _, ch := range row {
			// The symbol is read back off the current visual rather than off
			// the input, which is what ttfx does. They are the same here,
			// because nothing has animated yet.
			symbol := ch.Animation.CurrentVisual().Symbol
			colors := Fg(mapping.At(ch.InputCoord, fallback))
			if dynamic {
				// Both channels come back, so a cell that arrived with a
				// background keeps it. Foreground alone would blank every
				// filled panel of a captured screen once the scroll landed.
				colors = ColorPair{}
				if ch.Animation.InputColors.HasFg || ch.Animation.InputColors.HasBg {
					colors = ch.Animation.InputColors
				}
			}
			ch.Animation.SetAppearance(symbol, colors, ch.UsesInputColors)
		}
		o.pendingRows = append(o.pendingRows, &overflowRow{characters: row, final: true})
	}

	o.delay = 0
	// One gradient stop per canvas row, near enough. The floor division is
	// upstream's and is what bands the ramp.
	steps := max(floorDiv(canvas.Top, max(1, len(o.config.OverflowGradientStops)-1)), 1)
	o.overflowGradient, err = NewGradientSteps(o.config.OverflowGradientStops, steps, false)
	if err != nil {
		return err
	}
	return nil
}

// Advance releases the next few rows, lifts everything on screen by a row, and
// runs one frame. It reports whether the effect is still going.
func (o *Overflow) Advance(e *Engine) bool {
	if len(o.pendingRows) == 0 {
		return false
	}
	if o.delay == 0 {
		spectrum := o.overflowGradient.Spectrum
		releases := e.Rng.IntBetween(1, o.config.OverflowSpeed)
		for i := 0; i < releases && len(o.pendingRows) > 0; i++ {
			for _, row := range o.activeRows {
				row.moveUp()
				if !row.final {
					// The gradient is read at the row the copy has climbed to,
					// so the whole row changes colour as it goes up.
					head := row.characters[0].Motion.CurrentCoord.Row
					row.setColor(Fg(spectrum[min(head, len(spectrum)-1)]))
				}
			}
			next := o.pendingRows[0]
			o.pendingRows = o.pendingRows[1:]
			next.setup()
			next.moveUp()
			if !next.final {
				next.setColor(Fg(spectrum[0]))
			}
			for _, ch := range next.characters {
				e.Terminal.SetCharacterVisibility(ch, true)
			}
			o.activeRows = append(o.activeRows, next)
		}
		o.delay = e.Rng.IntBetween(0, 3)
	} else {
		o.delay--
	}

	// A row that has climbed past the top of the canvas is off screen for
	// good. It stays visible and keeps its coordinate; it is just no longer
	// worth walking every frame.
	kept := o.activeRows[:0]
	for _, row := range o.activeRows {
		if row.characters[0].Motion.CurrentCoord.Row <= e.Terminal.Canvas.Top {
			kept = append(kept, row)
		}
	}
	o.activeRows = kept

	e.Update()
	return true
}
