package tuiffects

import "sort"

// Effect is one animation. Build runs once and sets up every character's
// scenes and paths; Advance then runs one frame at a time until it reports
// that the effect is over.
//
// Advance does not return the frame. The caller reads it from the engine with
// Frame or FrameRows, so an effect never has to care whether its host wants an
// ANSI string or a cell grid.
type Effect interface {
	Build(e *Engine) error
	Advance(e *Engine) bool
}

// Factory builds a fresh instance of an effect with its default settings.
type Factory func() Effect

// Descriptor is a registered effect: its name, one line about what it does,
// how to build one, and what it needs from the terminal before it can build.
type Descriptor struct {
	Name        string
	Description string
	New         Factory
	// NeedsFillCharacters says the effect animates the empty cells of the
	// canvas as well as the input, so the terminal must be built with
	// MakeFillCharacters set.
	//
	// It is declared here rather than discovered in Build because the
	// terminal is built before the effect is, and a fill character cannot be
	// added afterwards. An effect that queries InnerFill or OuterFill without
	// setting this gets an empty result and quietly animates nothing, which
	// is the failure this field exists to prevent.
	NeedsFillCharacters bool
}

var registry = map[string]Descriptor{}

// Register adds an effect to the registry. It is meant to be called from an
// init function, and it panics on a duplicate name because that is a
// programming mistake rather than a runtime condition.
func Register(d Descriptor) {
	if _, exists := registry[d.Name]; exists {
		panic("tuiffects: effect " + d.Name + " is registered twice")
	}
	registry[d.Name] = d
}

// Lookup finds a registered effect by name.
func Lookup(name string) (Descriptor, bool) {
	d, ok := registry[name]
	return d, ok
}

// Names lists every registered effect, sorted.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Descriptors lists every registered effect with its description, sorted by
// name.
func Descriptors() []Descriptor {
	names := Names()
	out := make([]Descriptor, 0, len(names))
	for _, name := range names {
		out = append(out, registry[name])
	}
	return out
}

// Run builds an effect and returns every frame it produces, capped at
// maxFrames. It exists for tests and for a caller that wants the whole
// animation up front rather than one frame per host frame.
func Run(effect Effect, e *Engine, maxFrames int) ([]string, error) {
	if err := effect.Build(e); err != nil {
		return nil, err
	}
	var frames []string
	for len(frames) < maxFrames {
		if !effect.Advance(e) {
			break
		}
		frames = append(frames, e.Frame())
	}
	return frames, nil
}

// overInputBackground puts fg on the background the input carried at coord.
//
// It exists because upstream animates piped text, where nothing carries a
// background, so anything an effect throws across the screen, a spark or a
// raindrop or a puff of smoke or a travelling digit, is drawn as a foreground
// on nothing. Over a captured screen that is a hole punched straight through
// whatever it is flying across, moving every frame: a title bar sparkles with
// gaps for the length of the run. A particle should pass in front of the
// picture, not through it.
//
// It is a no-op outside DynamicExistingColors, so the default behaviour of
// every effect that calls it stays exactly upstream's. For the usual case, one
// call covering everything the effect put on the screen itself, use
// carryAddedCharactersOverBackgrounds below.
func overInputBackground(e *Engine, coord Coord, fg Color) ColorPair {
	pair := Fg(fg)
	if e.Terminal.Config.ExistingColorHandling != DynamicExistingColors {
		return pair
	}
	under := e.Terminal.CharacterAtInputCoord(coord)
	if under != nil && under.IsVisible && under.Animation.InputColors.HasBg {
		pair.Bg, pair.HasBg = under.Animation.InputColors.Bg, true
	}
	return pair
}

// carryAddedCharactersOverBackgrounds repaints every character the effect
// added, meaning particles, beams, digits and the cells of a lightning strike,
// over the background of the cell it is currently standing on.
//
// Call it from Advance, after Engine.Update, so it paints over the frame the
// scenes have just produced. It is a no-op outside DynamicExistingColors.
func carryAddedCharactersOverBackgrounds(e *Engine) {
	if e.Terminal.Config.ExistingColorHandling != DynamicExistingColors {
		return
	}
	for _, ch := range e.Terminal.AddedCharacters {
		if !ch.IsVisible {
			continue
		}
		visual := ch.Animation.CurrentVisual()
		if !visual.Colors.HasFg {
			continue
		}
		ch.Animation.SetAppearance(
			visual.Symbol,
			overInputBackground(e, ch.Motion.CurrentCoord, visual.Colors.Fg),
			ch.UsesInputColors)
	}
}
