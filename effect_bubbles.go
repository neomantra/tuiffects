package tuiffects

// bubbles, ported from ttfx src/effects/bubbles.rs, which ports
// TerminalTextEffects effects/effect_bubbles.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "bubbles",
		Description: "Groups of characters ride a bubble down the screen, which pops and drops them into place",
		New:         func() Effect { return NewBubbles(DefaultBubblesConfig()) },
	})
}

// PopCondition decides when a floating bubble bursts.
type PopCondition int

// The three pop conditions. Row is first so the zero value is upstream's
// default.
const (
	// PopOnRow bursts a bubble once it reaches the lowest input row of the
	// characters riding it, so each group pops near where it belongs.
	PopOnRow PopCondition = iota
	// PopOnBottom carries every bubble down to the bottom of the canvas
	// first.
	PopOnBottom
	// PopAnywhere is PopOnBottom with a small chance of bursting on any
	// frame along the way.
	PopAnywhere
)

var popConditionNames = map[string]PopCondition{
	"row":      PopOnRow,
	"bottom":   PopOnBottom,
	"anywhere": PopAnywhere,
}

// ParsePopCondition looks up a pop condition by its upstream name.
func ParsePopCondition(name string) (PopCondition, bool) {
	c, ok := popConditionNames[name]
	return c, ok
}

// BubblesConfig tunes the bubbles effect.
type BubblesConfig struct {
	// Rainbow makes each bubble cycle a rainbow instead of wearing one
	// colour. BubbleColors is ignored when it is set.
	Rainbow bool
	// BubbleColors are picked at random, one per bubble.
	BubbleColors []Color
	// PopColor is the colour of the burst, and the colour every character
	// ramps away from as it settles.
	PopColor Color
	// BubbleSpeed is how fast a bubble drifts down the canvas.
	BubbleSpeed float64
	// BubbleDelay is how many frames pass between one bubble being released
	// and the next.
	BubbleDelay int
	// PopCondition decides when a bubble bursts.
	PopCondition PopCondition
	// MovementEasing is upstream's flag of the same name. Neither ttfx nor
	// the Python it came from reads it: the burst uses out_expo and the drop
	// into place uses in_out_expo, both fixed. It is kept so the config still
	// matches upstream's command line, and setting it does nothing.
	MovementEasing Easing
	// FinalGradientStops colour the text once it has settled. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultBubblesConfig is upstream's default bubbles.
func DefaultBubblesConfig() BubblesConfig {
	return BubblesConfig{
		Rainbow: false,
		BubbleColors: []Color{
			MustParseColor("d33aff"), MustParseColor("7395c4"),
			MustParseColor("43c2a7"), MustParseColor("02ff7f"),
		},
		PopColor:       MustParseColor("ffffff"),
		BubbleSpeed:    0.5,
		BubbleDelay:    20,
		PopCondition:   PopOnRow,
		MovementEasing: InOutSine,
		FinalGradientStops: []Color{
			MustParseColor("d33aff"), MustParseColor("02ff7f"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Diagonal,
	}
}

// bubble is one floating bubble: the characters riding its rim, an invisible
// anchor character that carries the path they orbit, and the row that bursts
// it.
type bubble struct {
	characters []*Character
	radius     int
	// anchor is a space added to the terminal purely to hold the drift path.
	// It is never shown, and it is never put in the active set: the effect
	// steps it by hand so the rim can be redrawn around it in the same frame.
	anchor    *Character
	lowestRow int
	landed    bool
}

// Bubbles gathers the input into groups of five to twenty characters, hangs
// each group on the rim of a bubble above the canvas, and drifts the bubbles
// down one at a time. A bubble bursts when it reaches its row, throws its
// characters outwards, and each one then flies to where it belongs and ramps
// from the burst colour into its final colour.
//
// It assembles the screen rather than passing over it. A character is hidden
// until the bubble carrying it is released, and that holds under every colour
// policy including DynamicExistingColors: the characters are the bubbles, so
// showing the finished picture from the first frame would deliver the screen
// before a single bubble had been let go.
type Bubbles struct {
	config BubblesConfig

	// waiting are the bubbles not yet released, in the order they were built.
	waiting []*bubble
	// animating are the bubbles currently drifting.
	animating []*bubble
	// rainbow is the spectrum a rainbow bubble cycles. Upstream builds it in
	// the constructor; it is built in Build here so a bad spectrum comes back
	// as an error rather than a panic. The colours are constants, so it
	// cannot actually fail.
	rainbow *Gradient

	stepsSinceLastBubble int
}

// NewBubbles builds the effect.
func NewBubbles(config BubblesConfig) *Bubbles {
	return &Bubbles{config: config}
}

// Build gives every character its burst, its settle ramp and its flight home,
// then packs the characters into bubbles.
func (b *Bubbles) Build(e *Engine) error {
	rainbow, err := NewGradientSteps([]Color{
		MustParseColor("e81416"), // red
		MustParseColor("ffa500"), // orange
		MustParseColor("faeb36"), // yellow
		MustParseColor("79c314"), // green
		MustParseColor("487de7"), // blue
		MustParseColor("4b369d"), // indigo
		MustParseColor("70369d"), // violet
	}, 5, false)
	if err != nil {
		return err
	}
	b.rainbow = rainbow

	gradient, err := NewGradient(b.config.FinalGradientStops, b.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := gradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		b.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := gradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		// A flying character passes over the ones already settled.
		ch.Layer = 1

		pop1 := ch.Animation.NewScene("pop_1", SceneOptions{
			UsesInputColors: ch.UsesInputColors, Frames: 1,
		})
		if err := pop1.AddFrame("*", 9, VisualParams{Colors: Fg(b.config.PopColor)}); err != nil {
			return err
		}
		pop2 := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors, Frames: 1,
		})
		if err := pop2.AddFrame("'", 9, VisualParams{Colors: Fg(b.config.PopColor)}); err != nil {
			return err
		}

		// The settle ramp runs from the burst colour to the colour the
		// character keeps. Nine frames, because a two-stop gradient of eight
		// steps yields nine colours.
		settle := ch.Animation.NewScene("", SceneOptions{
			UsesInputColors: ch.UsesInputColors, Frames: 9,
		})
		if dynamic {
			// Both channels ramp, so a cell that carried a background picks
			// it back up rather than being left transparent. That is
			// upstream's dynamic branch, and it is what a captured screen
			// needs.
			var fgRamp, bgRamp *Gradient
			if ch.Animation.InputColors.HasFg {
				fgRamp, err = NewGradientSteps(
					[]Color{b.config.PopColor, ch.Animation.InputColors.Fg}, 8, false)
				if err != nil {
					return err
				}
			}
			if ch.Animation.InputColors.HasBg {
				bgRamp, err = NewGradientSteps(
					[]Color{b.config.PopColor, ch.Animation.InputColors.Bg}, 8, false)
				if err != nil {
					return err
				}
			}
			if fgRamp == nil && bgRamp == nil {
				if err := settle.AddFrame(ch.InputSymbol, 6, VisualParams{}); err != nil {
					return err
				}
			} else if err := settle.ApplyGradientToSymbols(
				[]string{ch.InputSymbol}, 6, fgRamp, bgRamp); err != nil {
				return err
			}
		} else {
			ramp, err := NewGradientSteps(
				[]Color{b.config.PopColor, mapping.At(ch.InputCoord, fallback)}, 8, false)
			if err != nil {
				return err
			}
			if err := settle.ApplyGradientToSymbols(
				[]string{ch.InputSymbol}, 6, ramp, nil); err != nil {
				return err
			}
		}

		ch.RegisterEvent(SceneComplete, SceneCaller(pop1.ID), ActivateScene(pop2.ID))
		ch.RegisterEvent(SceneComplete, SceneCaller(pop2.ID), ActivateScene(settle.ID))

		final, err := ch.Motion.NewPath("final", PathOptions{
			Speed: 0.3, Ease: InOutExpo, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := final.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
			return err
		}
		// Back down to the settled layer once it has arrived.
		ch.RegisterEvent(PathComplete, PathCaller(final.ID), SetLayer(0))
	}

	// The bubbles are filled from the bottom row of the input upwards, so the
	// group riding one bubble is a run of neighbouring characters.
	var unbubbled []*Character
	for _, group := range e.Terminal.GetCharactersGrouped(InputOnly(), GroupRowBottomToTop) {
		unbubbled = append(unbubbled, group...)
	}
	b.waiting = nil
	for len(unbubbled) > 0 {
		var group []*Character
		if len(unbubbled) < 5 {
			group = unbubbled
			unbubbled = nil
		} else {
			count := e.Rng.IntBetween(5, min(len(unbubbled), 20))
			group = append(group, unbubbled[:count]...)
			unbubbled = unbubbled[count:]
		}
		origin := C(e.Rng.IntBetween(canvas.Left, canvas.Right), canvas.Top+10)
		next, err := b.makeBubble(e, origin, group)
		if err != nil {
			return err
		}
		b.waiting = append(b.waiting, next)
	}
	b.animating = nil
	b.stepsSinceLastBubble = 0
	return nil
}

// makeBubble hangs a group of characters on a rim around a new anchor, points
// the anchor at the row that will burst it, and gives every character on the
// rim its sheen.
func (b *Bubbles) makeBubble(e *Engine, origin Coord, characters []*Character) (*bubble, error) {
	canvas := e.Terminal.Canvas
	radius := max(len(characters)/5, 1)
	anchor := e.Terminal.AddCharacter(" ", origin)

	lowestRow := canvas.Bottom
	if b.config.PopCondition == PopOnRow {
		lowestRow = characters[0].InputCoord.Row
		for _, ch := range characters {
			lowestRow = min(lowestRow, ch.InputCoord.Row)
		}
	}

	next := &bubble{characters: characters, radius: radius, anchor: anchor, lowestRow: lowestRow}
	b.setCharacterCoordinates(e, next)
	// Placing the rim is not landing on it. The bubble starts ten rows above
	// the canvas, so this only clears the flag the placement may have set.
	next.landed = false

	// The bubble drifts down to a column of its own rather than straight
	// down, so a run of bubbles does not fall in a line.
	column := e.Rng.IntBetween(canvas.Left, canvas.Right)
	drift, err := anchor.Motion.NewPath("", PathOptions{Speed: b.config.BubbleSpeed})
	if err != nil {
		return nil, err
	}
	if _, err := drift.NewWaypoint(C(column, lowestRow), nil, ""); err != nil {
		return nil, err
	}
	e.ActivatePath(anchor, drift.ID)

	if b.config.Rainbow {
		// Every character on the rim cycles the whole rainbow, each starting
		// two colours further round than the one before, so the sheen appears
		// to run around the bubble.
		spectrum := append([]Color(nil), b.rainbow.Spectrum...)
		offset := 0
		for _, ch := range next.characters {
			sheen := ch.Animation.NewScene("", SceneOptions{
				UsesInputColors: ch.UsesInputColors, Frames: len(spectrum),
			})
			for _, step := range spectrum {
				if err := sheen.AddFrame(ch.InputSymbol, 4, VisualParams{Colors: Fg(step)}); err != nil {
					return nil, err
				}
			}
			offset = (offset + 2) % len(spectrum)
			spectrum = append(append([]Color(nil), spectrum[offset:]...), spectrum[:offset]...)
			e.ActivateScene(ch, sheen.ID)
			// Upstream turns the loop on only after activating, which is why
			// the scene still starts on its first colour.
			sheen.IsLooping = true
		}
	} else {
		color := *Choice(e.Rng, b.config.BubbleColors)
		for _, ch := range next.characters {
			sheen := ch.Animation.NewScene("", SceneOptions{
				UsesInputColors: ch.UsesInputColors, Frames: 1,
			})
			if err := sheen.AddFrame(ch.InputSymbol, 1, VisualParams{Colors: Fg(color)}); err != nil {
				return nil, err
			}
			e.ActivateScene(ch, sheen.ID)
		}
	}
	return next, nil
}

// setCharacterCoordinates redraws the rim around wherever the anchor now
// stands, and notes whether any of it touched the bursting row.
func (b *Bubbles) setCharacterCoordinates(e *Engine, next *bubble) {
	// Not unique, so there is exactly one point per character.
	points := FindCoordsOnCircle(next.anchor.Motion.CurrentCoord, next.radius, len(next.characters), false)
	for i, ch := range next.characters {
		point := points[i]
		ch.Motion.SetCoordinate(point)
		if point.Row == next.lowestRow {
			next.landed = true
		}
	}
	if b.config.PopCondition == PopAnywhere && e.Rng.Float() < 0.002 {
		next.landed = true
	}
}

// pop throws the rim outwards. Each character flies to a point on a wider
// circle, and arriving there starts its flight home.
func (b *Bubbles) pop(e *Engine, next *bubble) {
	points := FindCoordsOnCircle(next.anchor.Motion.CurrentCoord, next.radius+3, len(next.characters), true)
	// Upstream zips the characters against the points, so a point the unique
	// filter dropped leaves the characters past the end of the list without a
	// pop_out path, and they never reach their final path either. That is
	// upstream's, and at these radii the filter drops nothing.
	for i, ch := range next.characters {
		if i >= len(points) {
			break
		}
		// A character rides exactly one bubble and a bubble pops once, so the
		// id cannot already be taken and the speed is a positive constant.
		out, err := ch.Motion.NewPath("pop_out", PathOptions{
			Speed: 0.3, Ease: OutExpo, HasEase: true,
		})
		if err != nil {
			continue
		}
		if _, err := out.NewWaypoint(points[i], nil, ""); err != nil {
			continue
		}
		ch.RegisterEvent(PathComplete, PathCaller("pop_out"), ActivatePath("final"))
	}
	for _, ch := range next.characters {
		e.ActivateScene(ch, "pop_1")
		e.ActivatePath(ch, "pop_out")
	}
}

// move drifts a bubble one step and redraws its rim around the new position.
//
// The rim is stepped by hand rather than through the active set, because the
// characters do not follow paths of their own while they float: they are
// placed from the anchor every frame.
func (b *Bubbles) move(e *Engine, next *bubble) {
	e.MotionMove(next.anchor)
	b.setCharacterCoordinates(e, next)
	for _, ch := range next.characters {
		e.StepAnimation(ch)
	}
}

// Advance releases a bubble when its delay is up, bursts the ones that have
// landed, drifts the rest, and runs one frame. It reports whether the effect
// is still going.
func (b *Bubbles) Advance(e *Engine) bool {
	if len(b.animating) == 0 && e.ActiveCount() == 0 && len(b.waiting) == 0 {
		return false
	}
	if len(b.waiting) > 0 && b.stepsSinceLastBubble >= b.config.BubbleDelay {
		next := b.waiting[0]
		b.waiting = b.waiting[1:]
		for _, ch := range next.characters {
			e.Terminal.SetCharacterVisibility(ch, true)
		}
		b.animating = append(b.animating, next)
		b.stepsSinceLastBubble = 0
	}
	b.stepsSinceLastBubble++

	// A burst bubble stops being a bubble: its characters join the active set
	// and the engine flies them home from here on.
	kept := b.animating[:0]
	for _, next := range b.animating {
		if next.landed {
			b.pop(e, next)
			for _, ch := range next.characters {
				e.Activate(ch)
			}
			continue
		}
		kept = append(kept, next)
	}
	b.animating = kept
	for _, next := range b.animating {
		b.move(e, next)
	}

	e.Update()
	return true
}
