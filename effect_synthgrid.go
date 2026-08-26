package tuiffects

// synthgrid, ported from ttfx src/effects/synthgrid.rs, which ports
// TerminalTextEffects effects/effect_synthgrid.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "synthgrid",
		Description: "A grid draws itself across the screen and fills its cells in block by block",
		New:         func() Effect { return NewSynthGrid(DefaultSynthGridConfig()) },
		// The grid divides the whole canvas, not just the text, and every cell
		// inside a block dissolves in. Without fill characters the blocks hold
		// only the input characters and the empty parts of the screen never
		// take part, which is most of what the effect looks like.
		NeedsFillCharacters: true,
	})
}

// SynthGridConfig tunes the synthgrid effect.
type SynthGridConfig struct {
	// GridGradientStops and GridGradientSteps colour the grid lines, painted
	// across the whole canvas rather than across the text.
	GridGradientStops []Color
	GridGradientSteps []int
	// GridGradientDirection is the axis the grid colour runs along.
	GridGradientDirection GradientDirection
	// TextGradientStops and TextGradientSteps colour the text once it
	// resolves. They are ignored when the engine is set to resolve to the
	// input's own colours.
	TextGradientStops []Color
	TextGradientSteps []int
	// TextGradientDirection is the axis the text colour runs along.
	TextGradientDirection GradientDirection
	// GridRowSymbol and GridColumnSymbol draw the horizontal and vertical
	// grid lines.
	GridRowSymbol    string
	GridColumnSymbol string
	// TextGenerationSymbols are the glyphs a cell flickers through before it
	// settles into its own character.
	TextGenerationSymbols []string
	// MaxActiveBlocks is the share of the grid's blocks allowed to be filling
	// in at once. At 0.1 a tenth of the screen is working at any moment, so
	// the fill reads as a sweep rather than as one flash.
	MaxActiveBlocks float64
}

// DefaultSynthGridConfig is upstream's default synthgrid.
func DefaultSynthGridConfig() SynthGridConfig {
	return SynthGridConfig{
		GridGradientStops:     []Color{MustParseColor("CC00CC"), MustParseColor("ffffff")},
		GridGradientSteps:     []int{12},
		GridGradientDirection: Diagonal,
		TextGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		TextGradientSteps:     []int{12},
		TextGradientDirection: Vertical,
		GridRowSymbol:         "─",
		GridColumnSymbol:      "│",
		TextGenerationSymbols: []string{"░", "▒", "▓"},
		MaxActiveBlocks:       0.1,
	}
}

// synthGridDirection is which way a grid line runs.
type synthGridDirection int

const (
	synthGridHorizontal synthGridDirection = iota
	synthGridVertical
)

// synthGridLine is one line of the grid: the characters that draw it, split
// into the ones already on screen and the ones still waiting.
//
// A horizontal line moves three characters per frame and a vertical line one,
// which is what makes both finish in roughly the same time on a screen that is
// wider than it is tall.
type synthGridLine struct {
	direction synthGridDirection
	// characters is the whole line in drawing order. Nothing reads it after
	// the line is built; it is kept because it is what the two working lists
	// are partitions of.
	characters []*Character
	collapsed  []*Character
	extended   []*Character
}

func (l *synthGridLine) stepCount() int {
	if l.direction == synthGridHorizontal {
		return 3
	}
	return 1
}

// extend shows the next few characters of the line.
func (l *synthGridLine) extend(e *Engine) {
	for i := 0; i < l.stepCount(); i++ {
		if len(l.collapsed) == 0 {
			continue
		}
		next := l.collapsed[0]
		l.collapsed = l.collapsed[1:]
		e.Terminal.SetCharacterVisibility(next, true)
		l.extended = append(l.extended, next)
	}
}

// collapse hides the next few characters of the line. The first call reverses
// the extended list, so the line retracts from the end it grew towards rather
// than from its origin.
func (l *synthGridLine) collapse(e *Engine) {
	if len(l.collapsed) == 0 {
		reverseCharacters(l.extended)
	}
	for i := 0; i < l.stepCount(); i++ {
		if len(l.extended) == 0 {
			continue
		}
		next := l.extended[0]
		l.extended = l.extended[1:]
		e.Terminal.SetCharacterVisibility(next, false)
		l.collapsed = append(l.collapsed, next)
	}
}

func (l *synthGridLine) isExtended() bool  { return len(l.collapsed) == 0 }
func (l *synthGridLine) isCollapsed() bool { return len(l.extended) == 0 }

// synthGridPhase is where the effect has got to.
type synthGridPhase int

const (
	synthGridExpand synthGridPhase = iota
	synthGridAddChars
	synthGridCollapse
	synthGridComplete
)

// synthGridGroup is one block of the grid: its number, which indexes the
// tracker, and the characters that fall inside it.
type synthGridGroup struct {
	number     int
	characters []*Character
}

// SynthGrid draws a grid over the canvas, then fills the blocks it made a few
// at a time, then takes the grid back down.
//
// It assembles the screen rather than passing over it: every character starts
// hidden and is shown when its block's turn comes. So under
// DynamicExistingColors it keeps upstream's hiding, unlike a sweep such as
// waves, which has to show the picture from the first frame. Showing
// everything up front here would leave nothing for the blocks to fill in.
type SynthGrid struct {
	config SynthGridConfig

	pendingGroups []synthGridGroup
	gridLines     []*synthGridLine
	// groupTracker counts how many characters of each block are still
	// resolving. A block's characters raise SceneComplete as they finish and
	// each one decrements its own entry.
	groupTracker    []int
	finalColors     map[*Character]ColorPair
	phase           synthGridPhase
	totalGroupCount int
	activeGroups    int
}

// NewSynthGrid builds the effect.
func NewSynthGrid(config SynthGridConfig) *SynthGrid {
	return &SynthGrid{
		config:      config,
		finalColors: map[*Character]ColorPair{},
		phase:       synthGridExpand,
	}
}

// synthGridEvenGap picks the spacing that divides a dimension most evenly
// while staying near a fifth of it.
//
// Upstream searches downwards from the dimension to 5, keeps every divisor
// that leaves a remainder of at most one, and takes the one closest to
// dimension // 5, with the first minimum winning a tie. Four is the fallback
// when nothing divides evenly enough.
func synthGridEvenGap(dimension int) int {
	dimension -= 2
	if dimension <= 0 {
		return 0
	}
	var potential []int
	for i := dimension; i > 4; i-- {
		if dimension%i <= 1 {
			potential = append(potential, i)
		}
	}
	if len(potential) == 0 {
		return 4
	}
	target := floorDiv(dimension, 5)
	best, bestKey := potential[0], abs(potential[0]-target)
	for _, gap := range potential[1:] {
		if key := abs(gap - target); key < bestKey {
			best, bestKey = gap, key
		}
	}
	return best
}

// makeGridLine builds one line of the grid. Its characters are added to the
// terminal rather than taken from the input, so they sit above the text on
// layer 2 and start hidden.
func (s *SynthGrid) makeGridLine(
	e *Engine, origin Coord, direction synthGridDirection, mapping CoordColorMap, fallback Color,
) (*synthGridLine, error) {
	canvas := e.Terminal.Canvas
	symbol := s.config.GridRowSymbol
	var coords []Coord
	if direction == synthGridHorizontal {
		coords = make([]Coord, 0, canvas.Right-canvas.Left+1)
		for column := canvas.Left; column <= canvas.Right; column++ {
			coords = append(coords, C(column, origin.Row))
		}
	} else {
		symbol = s.config.GridColumnSymbol
		coords = make([]Coord, 0, max(canvas.Top-canvas.Bottom, 0))
		// The top row is left out, which is upstream's: the top border line is
		// horizontal and would otherwise be crossed by every column.
		for row := canvas.Bottom; row < canvas.Top; row++ {
			coords = append(coords, C(origin.Column, row))
		}
	}

	characters := make([]*Character, 0, len(coords))
	for _, coord := range coords {
		ch := e.Terminal.AddCharacter(symbol, C(0, 0))
		scene := ch.Animation.NewScene("", SceneOptions{UsesInputColors: ch.UsesInputColors, Frames: 1})
		if err := scene.AddFrame(symbol, 1, VisualParams{Colors: Fg(mapping.At(coord, fallback))}); err != nil {
			return nil, err
		}
		e.ActivateScene(ch, scene.ID)
		ch.Layer = 2
		ch.Motion.SetCoordinate(coord)
		characters = append(characters, ch)
	}

	collapsed := make([]*Character, len(characters))
	copy(collapsed, characters)
	return &synthGridLine{direction: direction, characters: characters, collapsed: collapsed}, nil
}

// Build lays out the grid, works out which characters fall in which block, and
// gives every character the scene that flickers it into place.
func (s *SynthGrid) Build(e *Engine) error {
	canvas := e.Terminal.Canvas

	gridGradient, err := NewGradient(s.config.GridGradientStops, s.config.GridGradientSteps, false)
	if err != nil {
		return err
	}
	// The grid is painted across the whole canvas, so its mapping covers the
	// canvas rather than the text block the text gradient uses.
	gridMapping, err := gridGradient.BuildCoordinateColorMapping(
		1, canvas.Top, 1, canvas.Right, s.config.GridGradientDirection)
	if err != nil {
		return err
	}
	gridFallback := gridGradient.Spectrum[0]

	textGradient, err := NewGradient(s.config.TextGradientStops, s.config.TextGradientSteps, false)
	if err != nil {
		return err
	}
	textMapping, err := textGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		s.config.TextGradientDirection)
	if err != nil {
		return err
	}
	textFallback := textGradient.Spectrum[0]

	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors
	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		var colors ColorPair
		switch {
		case dynamic:
			// The character resolves back to the colours it arrived with,
			// background included, so a filled panel or a selection bar is
			// whole again once its block finishes.
			colors = ch.Animation.InputColors
		case ch.InputSymbol != " ":
			colors = Fg(textMapping.At(ch.InputCoord, textFallback))
		}
		s.finalColors[ch] = colors
	}

	// The four border lines first: the bottom and top rows, then the left and
	// right columns.
	for _, border := range []struct {
		origin    Coord
		direction synthGridDirection
	}{
		{C(canvas.Left, canvas.Bottom), synthGridHorizontal},
		{C(canvas.Left, canvas.Top), synthGridHorizontal},
		{C(canvas.Left, canvas.Bottom), synthGridVertical},
		{C(canvas.Right, canvas.Bottom), synthGridVertical},
	} {
		line, err := s.makeGridLine(e, border.origin, border.direction, gridMapping, gridFallback)
		if err != nil {
			return err
		}
		s.gridLines = append(s.gridLines, line)
	}

	// A canvas more than twice as tall as it is wide is divided by its rows
	// and one that is not is divided by its columns, so the blocks stay
	// roughly square in a cell grid that is not.
	var rowGap, columnGap int
	if canvas.Top > 2*canvas.Right {
		rowGap = synthGridEvenGap(canvas.Top) + 1
		columnGap = rowGap * 2
	} else {
		columnGap = synthGridEvenGap(canvas.Right) + 1
		rowGap = floorDiv(columnGap, 2)
	}

	var rowIndexes, columnIndexes []int
	rowStep := max(rowGap, 1)
	for rowIndex := canvas.Bottom + rowGap; rowIndex < canvas.Top; rowIndex += rowStep {
		if canvas.Top-rowIndex < 2 {
			continue
		}
		rowIndexes = append(rowIndexes, rowIndex)
		line, err := s.makeGridLine(
			e, C(canvas.Left, rowIndex), synthGridHorizontal, gridMapping, gridFallback)
		if err != nil {
			return err
		}
		s.gridLines = append(s.gridLines, line)
	}
	columnStep := max(columnGap, 1)
	for columnIndex := canvas.Left + columnGap; columnIndex < canvas.Right; columnIndex += columnStep {
		if canvas.Right-columnIndex < 2 {
			continue
		}
		columnIndexes = append(columnIndexes, columnIndex)
		line, err := s.makeGridLine(
			e, C(columnIndex, canvas.Bottom), synthGridVertical, gridMapping, gridFallback)
		if err != nil {
			return err
		}
		s.gridLines = append(s.gridLines, line)
	}

	// One past the far edge closes the last block on each axis.
	rowIndexes = append(rowIndexes, canvas.Top+1)
	columnIndexes = append(columnIndexes, canvas.Right+1)

	prevRow := 1
	for _, rowIndexValue := range rowIndexes {
		// Upstream reassigns row_index inside the column loop and lets the
		// changed value carry into prevRow, so the top row joins the last
		// band of blocks rather than falling outside every one of them.
		rowIndex := rowIndexValue
		prevColumn := 1
		for _, columnIndex := range columnIndexes {
			if rowIndex == canvas.Top {
				rowIndex++
			}
			var inBlock []*Character
			for row := prevRow; row < rowIndex; row++ {
				for column := prevColumn; column < columnIndex; column++ {
					if ch := e.Terminal.CharacterAtInputCoord(C(column, row)); ch != nil {
						inBlock = append(inBlock, ch)
					}
				}
			}
			if len(inBlock) > 0 {
				s.pendingGroups = append(s.pendingGroups, synthGridGroup{
					number: len(s.pendingGroups), characters: inBlock,
				})
			}
			prevColumn = columnIndex
		}
		prevRow = rowIndex
	}

	s.groupTracker = make([]int, len(s.pendingGroups))
	for _, group := range s.pendingGroups {
		number := group.number
		for _, ch := range group.characters {
			frameCount := e.Rng.IntBetween(15, 30)
			dissolve := ch.Animation.NewScene("", SceneOptions{
				UsesInputColors: ch.UsesInputColors,
				Frames:          frameCount + 1,
			})
			for i := 0; i < frameCount; i++ {
				symbol := *Choice(e.Rng, s.config.TextGenerationSymbols)
				fg := *Choice(e.Rng, textGradient.Spectrum)
				if err := dissolve.AddFrame(symbol, 2, VisualParams{Colors: Fg(fg)}); err != nil {
					return err
				}
			}
			if err := dissolve.AddFrame(ch.InputSymbol, 1, VisualParams{Colors: s.finalColors[ch]}); err != nil {
				return err
			}
			e.ActivateScene(ch, dissolve.ID)
			ch.RegisterEvent(SceneComplete, SceneCaller(dissolve.ID),
				Callback(func(*Engine, *Character) { s.groupTracker[number]-- }))
		}
	}

	// The blocks are built in reading order and filled in a random one, so the
	// screen comes back in patches rather than in lines.
	Shuffle(e.Rng, s.pendingGroups)
	s.phase = synthGridExpand
	s.totalGroupCount = len(s.pendingGroups)
	if s.totalGroupCount == 0 {
		// Nothing to fill in, so there is nothing to hide either.
		for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
			e.Terminal.SetCharacterVisibility(ch, true)
			e.Activate(ch)
		}
	}
	s.activeGroups = 0
	return nil
}

// Advance runs one frame of whichever phase the effect is in and reports
// whether it is still going.
func (s *SynthGrid) Advance(e *Engine) bool {
	if len(s.pendingGroups) == 0 && e.ActiveCount() == 0 && s.phase == synthGridComplete {
		return false
	}

	switch s.phase {
	case synthGridExpand:
		if extended := s.allLinesExtended(); !extended {
			for _, line := range s.gridLines {
				if !line.isExtended() {
					line.extend(e)
				}
			}
		} else {
			s.phase = synthGridAddChars
		}
	case synthGridAddChars:
		if len(s.pendingGroups) > 0 &&
			float64(s.activeGroups) < float64(s.totalGroupCount)*s.config.MaxActiveBlocks {
			next := s.pendingGroups[0]
			s.pendingGroups = s.pendingGroups[1:]
			for _, ch := range next.characters {
				e.Terminal.SetCharacterVisibility(ch, true)
				e.Activate(ch)
				s.groupTracker[next.number]++
			}
		}
		if len(s.pendingGroups) == 0 && e.ActiveCount() == 0 && s.activeGroups == 0 {
			s.phase = synthGridCollapse
		}
	case synthGridCollapse:
		if collapsed := s.allLinesCollapsed(); !collapsed {
			for _, line := range s.gridLines {
				if !line.isCollapsed() {
					line.collapse(e)
				}
			}
		} else {
			s.phase = synthGridComplete
		}
	case synthGridComplete:
	}

	e.Update()
	s.activeGroups = 0
	for _, count := range s.groupTracker {
		if count != 0 {
			s.activeGroups++
		}
	}
	return true
}

func (s *SynthGrid) allLinesExtended() bool {
	for _, line := range s.gridLines {
		if !line.isExtended() {
			return false
		}
	}
	return true
}

func (s *SynthGrid) allLinesCollapsed() bool {
	for _, line := range s.gridLines {
		if !line.isCollapsed() {
			return false
		}
	}
	return true
}
