package tuiffects

// matrix, ported from ttfx src/effects/matrix.rs, which ports
// TerminalTextEffects effects/effect_matrix.py by ChrisBuilds.
//
// This is one of the two effects written in seconds rather than frames: the
// rain falls for RainTime seconds of engine clock, then the columns fill the
// screen solid and the text resolves out of them. See clock.go for why the
// engine's clock is virtual, and why that keeps a seeded run repeatable.
//
// Matrix ASSEMBLES the screen rather than passing over it. Every character
// starts hidden, the rain shows it as a falling glyph, and the resolve phase
// turns it back into itself. So the DynamicExistingColors deviation the sweep
// effects need does not apply here: showing every character from frame one
// would put the finished picture behind the rain and there would be nothing
// left to resolve. Dynamic mode only changes the colour each character
// resolves to, which is upstream's own dynamic branch.

func init() {
	Register(Descriptor{
		Name:                "matrix",
		Description:         "Green rain falls down the screen and the text resolves out of it",
		New:                 func() Effect { return NewMatrix(DefaultMatrixConfig()) },
		NeedsFillCharacters: true,
	})
}

// MatrixConfig tunes the matrix effect.
type MatrixConfig struct {
	// HighlightColor is the colour of the character at the bottom of a
	// falling column, the drop's leading edge.
	HighlightColor Color
	// RainColorGradient is the ramp the rest of a column is coloured from.
	// Colours are picked from it at random.
	RainColorGradient []Color
	// RainSymbols are the glyphs the rain is drawn with.
	RainSymbols []string
	// RainFallDelayLow and RainFallDelayHigh bound how many frames a column
	// waits between rows. A column picks one value and keeps it.
	RainFallDelayLow  int
	RainFallDelayHigh int
	// RainColumnDelayLow and RainColumnDelayHigh bound how many frames pass
	// between starting new columns.
	RainColumnDelayLow  int
	RainColumnDelayHigh int
	// RainTime is how many seconds the rain falls for before the columns fill
	// the screen and the text resolves. Zero means the rain never ends on its
	// own.
	RainTime int
	// SymbolSwapChance and ColorSwapChance are the per-frame odds that a
	// character already in the rain changes its glyph or its colour.
	SymbolSwapChance float64
	ColorSwapChance  float64
	// ResolveDelay is how many frames pass between resolving one group of
	// characters and the next. Raise it to slow the ending down.
	ResolveDelay int
	// FinalGradientStops colour the text once it resolves. They are ignored
	// when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientFrames    int
	FinalGradientDirection GradientDirection
}

// DefaultMatrixConfig is upstream's default matrix.
func DefaultMatrixConfig() MatrixConfig {
	return MatrixConfig{
		HighlightColor:    MustParseColor("dbffdb"),
		RainColorGradient: []Color{MustParseColor("92be92"), MustParseColor("185318")},
		RainSymbols: []string{
			"2", "5", "9", "8", "Z", "*", ")", ":", ".", "\"", "=", "+", "-", "¦", "|", "_",
			"ｦ", "ｱ", "ｳ", "ｴ", "ｵ", "ｶ", "ｷ", "ｹ", "ｺ", "ｻ", "ｼ", "ｽ", "ｾ", "ｿ", "ﾀ", "ﾂ",
			"ﾃ", "ﾅ", "ﾆ", "ﾇ", "ﾈ", "ﾊ", "ﾋ", "ﾎ", "ﾏ", "ﾐ", "ﾑ", "ﾒ", "ﾓ", "ﾔ", "ﾕ", "ﾗ",
			"ﾘ", "ﾜ",
		},
		RainFallDelayLow:    2,
		RainFallDelayHigh:   15,
		RainColumnDelayLow:  3,
		RainColumnDelayHigh: 9,
		RainTime:            15,
		SymbolSwapChance:    0.005,
		ColorSwapChance:     0.001,
		ResolveDelay:        3,
		FinalGradientStops: []Color{
			MustParseColor("92be92"), MustParseColor("336b33"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientFrames:    3,
		FinalGradientDirection: Radial,
	}
}

// matrixSetAppearance is upstream's character.animation.set_appearance. It is
// how the rain draws: the rain never runs a scene, it overwrites the current
// visual every frame.
func matrixSetAppearance(ch *Character, symbol string, colors ColorPair) {
	ch.Animation.SetAppearance(symbol, colors, ch.UsesInputColors)
}

// matrixHasInputColors reports whether the character arrived carrying a colour
// of its own. A blank cell that carried a background is still worth resolving,
// because on a captured screen that is the window chrome.
func matrixHasInputColors(ch *Character) bool {
	return ch.Animation.InputColors.HasFg || ch.Animation.InputColors.HasBg
}

// matrixColumnPhase is what a single column is doing. It is not the same as
// the effect's phase: when the effect turns to filling, the columns already
// raining keep falling until they run out.
type matrixColumnPhase int

const (
	// matrixColumnRain drops a partial column that trims itself from the top
	// and slides off the bottom.
	matrixColumnRain matrixColumnPhase = iota
	// matrixColumnFill runs a column all the way down and leaves it there.
	matrixColumnFill
)

// matrixRainColumn is one column of falling characters, upstream's inner
// RainColumn class.
//
// ttfx keeps these in a Vec and passes indices around because its characters
// live in an arena. Here a column is a pointer, and the pending, active and
// full lists hold pointers, which is the identity comparison upstream's
// `column not in self.full_columns` is doing anyway.
type matrixRainColumn struct {
	// characters is the whole column, top to bottom. It never changes.
	characters []*Character
	// pending are the characters not yet dropped, in the order they fall.
	pending []*Character
	// visible are the characters currently on screen, head of the drop last.
	visible []*Character

	phase matrixColumnPhase
	// dropChance is the per-tick odds the whole column slides down a row.
	// The effect raises it to 1 when the rain is being wound up.
	dropChance float64
	// baseFallDelay is the frames between rows, activeFallDelay the countdown.
	baseFallDelay   int
	activeFallDelay int
	// length is how many characters the column shows at once.
	length int
	// holdTime keeps a full-height column still for a moment before it starts
	// eating itself from the top.
	holdTime int
}

func newMatrixRainColumn(e *Engine, config *MatrixConfig, characters []*Character) *matrixRainColumn {
	column := &matrixRainColumn{characters: characters, dropChance: 0.08}
	column.setup(e, config, matrixColumnRain)
	return column
}

// setup puts the column back at the top with nothing on screen, upstream's
// RainColumn.setup_column.
func (c *matrixRainColumn) setup(e *Engine, config *MatrixConfig, phase matrixColumnPhase) {
	c.pending = c.pending[:0]
	c.phase = phase
	for _, ch := range c.characters {
		e.Terminal.SetCharacterVisibility(ch, false)
		c.pending = append(c.pending, ch)
		ch.Motion.CurrentCoord = ch.InputCoord
	}
	c.visible = c.visible[:0]
	if c.phase == matrixColumnFill {
		// A filling column falls about three times as fast as a raining one.
		c.baseFallDelay = e.Rng.IntBetween(
			max(floorDiv(config.RainFallDelayLow, 3), 1),
			max(floorDiv(config.RainFallDelayHigh, 3), 1))
	} else {
		c.baseFallDelay = e.Rng.IntBetween(config.RainFallDelayLow, config.RainFallDelayHigh)
	}
	c.activeFallDelay = 0
	if c.phase == matrixColumnRain {
		c.length = e.Rng.IntBetween(max(1, int(float64(len(c.characters))*0.1)), len(c.characters))
	} else {
		c.length = len(c.characters)
	}
	c.holdTime = 0
	if c.length == len(c.characters) {
		c.holdTime = e.Rng.IntBetween(20, 45)
	}
}

// trim drops the character at the top of the column and dims the one that is
// now at the top, so the tail of the drop fades out.
func (c *matrixRainColumn) trim(e *Engine, rainColors []Color) {
	if len(c.visible) == 0 {
		return
	}
	e.Terminal.SetCharacterVisibility(c.visible[0], false)
	c.visible = c.visible[1:]
	if len(c.visible) > 1 {
		c.fadeLast(e, rainColors)
	}
}

// drop slides every visible character down one row and lets the ones that fall
// off the bottom of the canvas go.
func (c *matrixRainColumn) drop(e *Engine) {
	bottom := e.Terminal.Canvas.Bottom
	kept := c.visible[:0]
	for _, ch := range c.visible {
		current := ch.Motion.CurrentCoord
		ch.Motion.CurrentCoord = C(current.Column, current.Row-1)
		if ch.Motion.CurrentCoord.Row < bottom {
			e.Terminal.SetCharacterVisibility(ch, false)
			continue
		}
		kept = append(kept, ch)
	}
	c.visible = kept
}

// fadeLast darkens the character at the top of the column, picking from the
// darkest end of the rain ramp.
func (c *matrixRainColumn) fadeLast(e *Engine, rainColors []Color) {
	tail := rainColors[max(len(rainColors)-3, 0):]
	darker := AdjustColorBrightness(*Choice(e.Rng, tail), 0.65)
	target := c.visible[0]
	matrixSetAppearance(target, target.Animation.CurrentVisual().Symbol, Fg(darker))
}

// resolveChar takes one character out of the column at random, so a full
// column resolves in a scatter rather than top to bottom.
func (c *matrixRainColumn) resolveChar(e *Engine) *Character {
	index := e.Rng.IntBetween(0, len(c.visible)-1)
	next := c.visible[index]
	c.visible = append(c.visible[:index], c.visible[index+1:]...)
	return next
}

// tick runs the column for one frame, upstream's RainColumn.tick.
func (c *matrixRainColumn) tick(e *Engine, config *MatrixConfig, rainColors []Color) {
	if c.activeFallDelay == 0 {
		switch {
		case len(c.pending) > 0:
			next := c.pending[0]
			c.pending = c.pending[1:]
			matrixSetAppearance(next, *Choice(e.Rng, config.RainSymbols), Fg(config.HighlightColor))
			// Only the head of the drop carries the highlight, so the one that
			// was the head goes back to a rain colour.
			if len(c.visible) > 0 {
				previous := c.visible[len(c.visible)-1]
				matrixSetAppearance(previous, previous.Animation.CurrentVisual().Symbol,
					Fg(*Choice(e.Rng, rainColors)))
			}
			e.Terminal.SetCharacterVisibility(next, true)
			c.visible = append(c.visible, next)
		case len(c.visible) > 0:
			last := c.visible[len(c.visible)-1]
			visual := last.Animation.CurrentVisual()
			if visual.Colors.HasFg && visual.Colors.Fg == config.HighlightColor {
				matrixSetAppearance(last, visual.Symbol, Fg(*Choice(e.Rng, rainColors)))
			}
			if c.holdTime != 0 {
				c.holdTime--
			} else if c.phase == matrixColumnRain {
				if e.Rng.Float() < c.dropChance {
					c.drop(e)
				}
				c.trim(e, rainColors)
			}
		}

		// A column that is still adding characters must not grow past the
		// length it was given.
		if len(c.visible) > c.length {
			c.trim(e, rainColors)
		}
		c.activeFallDelay = c.baseFallDelay
	} else {
		c.activeFallDelay--
	}

	// Both chances are drawn for every visible character every frame, in
	// upstream's order, even when neither one changes anything.
	for _, ch := range c.visible {
		var nextSymbol string
		haveSymbol := false
		if e.Rng.Float() < config.SymbolSwapChance {
			nextSymbol = *Choice(e.Rng, config.RainSymbols)
			haveSymbol = true
		}
		var nextColor Color
		haveColor := false
		if e.Rng.Float() < config.ColorSwapChance {
			nextColor = *Choice(e.Rng, rainColors)
			haveColor = true
		}
		if !haveSymbol && !haveColor {
			continue
		}
		visual := ch.Animation.CurrentVisual()
		symbolSame := !haveSymbol || nextSymbol == visual.Symbol
		colorSame := !haveColor || (visual.Colors.HasFg && visual.Colors.Fg == nextColor)
		if symbolSame && colorSame {
			continue
		}
		symbol := visual.Symbol
		if haveSymbol {
			symbol = nextSymbol
		}
		colors := ColorPair{}
		switch {
		case haveColor:
			colors = Fg(nextColor)
		case visual.Colors.HasFg:
			colors = Fg(visual.Colors.Fg)
		}
		matrixSetAppearance(ch, symbol, colors)
	}
}

// matrixPhase is what the effect as a whole is doing.
type matrixPhase int

const (
	// matrixPhaseRain runs partial columns for RainTime seconds.
	matrixPhaseRain matrixPhase = iota
	// matrixPhaseFill runs every column down to the bottom and leaves it.
	matrixPhaseFill
	// matrixPhaseResolve turns the filled screen back into the input.
	matrixPhaseResolve
)

// Matrix rains characters down the screen for a while, fills the screen with
// them, then resolves the input out of the fill a few characters at a time.
type Matrix struct {
	config MatrixConfig

	pendingColumns []*matrixRainColumn
	activeColumns  []*matrixRainColumn
	fullColumns    []*matrixRainColumn

	// rainColors is the rain gradient's spectrum, drawn from at random.
	rainColors []Color

	columnDelay     int
	resolveDelay    int
	finalFrameShown bool
	rainComplete    bool
	phase           matrixPhase
	// rainStart is the clock reading taken at the end of Build, which the
	// rain deadline is measured from.
	rainStart float64
}

// NewMatrix builds the effect.
func NewMatrix(config MatrixConfig) *Matrix {
	return &Matrix{config: config, resolveDelay: config.ResolveDelay}
}

// Build gives every input character the scene it resolves through, then cuts
// the whole canvas into columns and shuffles them.
func (m *Matrix) Build(e *Engine) error {
	rainGradient, err := NewGradientSteps(m.config.RainColorGradient, 6, false)
	if err != nil {
		return err
	}
	m.rainColors = rainGradient.Spectrum

	finalGradient, err := NewGradient(m.config.FinalGradientStops, m.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		m.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		if dynamic {
			// Upstream's dynamic branch. A character that carried no colour at
			// all resolves to no colour, which is why this is not guarded on
			// UsesInputColors.
			final = ch.Animation.InputColors
		}
		resolve := ch.Animation.NewScene("resolve", SceneOptions{UsesInputColors: ch.UsesInputColors})
		var fgGradient, bgGradient *Gradient
		if final.HasFg {
			if fgGradient, err = NewGradientSteps(
				[]Color{m.config.HighlightColor, final.Fg}, 8, false); err != nil {
				return err
			}
		}
		if final.HasBg {
			if bgGradient, err = NewGradientSteps(
				[]Color{m.config.HighlightColor, final.Bg}, 8, false); err != nil {
				return err
			}
		}
		if fgGradient == nil && bgGradient == nil {
			if err := resolve.AddFrame(
				ch.InputSymbol, m.config.FinalGradientFrames, VisualParams{}); err != nil {
				return err
			}
			continue
		}
		if err := resolve.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, m.config.FinalGradientFrames, fgGradient, bgGradient); err != nil {
			return err
		}
	}

	// The rain runs over the whole canvas, not over the text, so the columns
	// are cut from every character the terminal holds.
	everything := CharacterFilter{Input: true, InnerFill: true, OuterFill: true}
	for _, columnChars := range e.Terminal.GetCharactersGrouped(everything, GroupColumnLeftToRight) {
		// Grouping hands back a column bottom to top, and the rain falls.
		reverseCharacters(columnChars)
		m.pendingColumns = append(m.pendingColumns, newMatrixRainColumn(e, &m.config, columnChars))
	}
	Shuffle(e.Rng, m.pendingColumns)

	m.rainStart = e.Clock.Wall()
	return nil
}

// columnPhase is the phase a column gets when the effect sets it up again. It
// is only ever read while the effect is raining or filling.
func (m *Matrix) columnPhase() matrixColumnPhase {
	if m.phase == matrixPhaseRain {
		return matrixColumnRain
	}
	return matrixColumnFill
}

// Advance runs one frame and reports whether the effect is still going.
func (m *Matrix) Advance(e *Engine) bool {
	switch m.phase {
	case matrixPhaseRain, matrixPhaseFill:
		m.advanceRain(e)
	case matrixPhaseResolve:
		m.advanceResolve(e)
	}

	if len(m.fullColumns) > 0 || len(m.activeColumns) > 0 || e.ActiveCount() > 0 ||
		len(m.pendingColumns) > 0 || !m.rainComplete {
		e.Update()
		return true
	}
	if !m.finalFrameShown {
		m.finalFrameShown = true
		e.Update()
		return true
	}
	return false
}

func (m *Matrix) advanceRain(e *Engine) {
	if m.columnDelay == 0 {
		if m.phase == matrixPhaseRain {
			// A few new columns start, then nothing for a few frames.
			for i, n := 0, e.Rng.IntBetween(1, 3); i < n && len(m.pendingColumns) > 0; i++ {
				m.activeColumns = append(m.activeColumns, m.pendingColumns[0])
				m.pendingColumns = m.pendingColumns[1:]
			}
			m.columnDelay = e.Rng.IntBetween(m.config.RainColumnDelayLow, m.config.RainColumnDelayHigh)
		} else {
			// Filling starts every column that is waiting, all at once.
			m.activeColumns = append(m.activeColumns, m.pendingColumns...)
			m.pendingColumns = m.pendingColumns[:0]
			m.columnDelay = 1
		}
	} else {
		m.columnDelay--
	}

	// Nothing below adds to or removes from activeColumns, so it is walked in
	// place rather than through the copy ttfx has to take.
	for _, column := range m.activeColumns {
		column.tick(e, &m.config, m.rainColors)
		if len(column.pending) > 0 {
			continue
		}
		if column.phase == matrixColumnFill && !matrixColumnListHas(m.fullColumns, column) {
			m.fullColumns = append(m.fullColumns, column)
		} else if len(column.visible) == 0 {
			column.setup(e, &m.config, m.columnPhase())
			m.pendingColumns = append(m.pendingColumns, column)
		}
	}
	m.activeColumns = matrixKeepColumnsWithVisible(m.activeColumns)

	if m.phase == matrixPhaseFill && len(m.pendingColumns) == 0 && m.everyActiveColumnIsFull() {
		m.phase = matrixPhaseResolve
		m.activeColumns = m.activeColumns[:0]
	}

	// The rain deadline, upstream's one wall-clock read per frame.
	if m.phase == matrixPhaseRain && m.config.RainTime > 0 &&
		e.Clock.Wall()-m.rainStart > float64(m.config.RainTime) {
		m.rainComplete = true
		m.phase = matrixPhaseFill
		for _, column := range m.activeColumns {
			// Wind the raining columns up: no more holding, and drop every
			// tick so they clear the canvas quickly.
			column.holdTime = 0
			column.dropChance = 1.0
		}
		for _, column := range m.pendingColumns {
			column.setup(e, &m.config, matrixColumnFill)
		}
	}
}

func (m *Matrix) advanceResolve(e *Engine) {
	// resolveDelay is one counter shared by every column, as upstream has it,
	// so the number of columns sets how fast the screen resolves.
	for _, column := range m.fullColumns {
		column.tick(e, &m.config, m.rainColors)
		if len(column.visible) == 0 {
			continue
		}
		if m.resolveDelay != 0 {
			m.resolveDelay--
			continue
		}
		for i, n := 0, e.Rng.IntBetween(1, 4); i < n; i++ {
			if len(column.visible) == 0 {
				break
			}
			next := column.resolveChar(e)
			if next.InputSymbol != " " || matrixHasInputColors(next) {
				e.ActivateScene(next, "resolve")
				e.Activate(next)
				continue
			}
			// A blank that carried no colour has nothing to resolve into, so
			// it just goes away.
			e.Terminal.SetCharacterVisibility(next, false)
		}
		m.resolveDelay = m.config.ResolveDelay
	}
	m.fullColumns = matrixKeepColumnsWithVisible(m.fullColumns)
}

func (m *Matrix) everyActiveColumnIsFull() bool {
	for _, column := range m.activeColumns {
		if len(column.pending) > 0 || column.phase != matrixColumnFill {
			return false
		}
	}
	return true
}

func matrixColumnListHas(columns []*matrixRainColumn, want *matrixRainColumn) bool {
	for _, column := range columns {
		if column == want {
			return true
		}
	}
	return false
}

func matrixKeepColumnsWithVisible(columns []*matrixRainColumn) []*matrixRainColumn {
	kept := columns[:0]
	for _, column := range columns {
		if len(column.visible) > 0 {
			kept = append(kept, column)
		}
	}
	return kept
}
