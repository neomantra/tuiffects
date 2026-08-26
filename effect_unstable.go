package tuiffects

// unstable, ported from ttfx src/effects/unstable.rs, which ports
// TerminalTextEffects effects/effect_unstable.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "unstable",
		Description: "Scrambles the characters, shakes them until they fly apart, then puts them back",
		New:         func() Effect { return NewUnstable(DefaultUnstableConfig()) },
	})
}

// UnstableConfig tunes the unstable effect.
type UnstableConfig struct {
	// UnstableColor is the colour a character heats up to while it rumbles.
	UnstableColor Color
	// ExplosionEase and ExplosionSpeed shape the flight out to the canvas edge.
	ExplosionEase  Easing
	ExplosionSpeed float64
	// ReassemblyEase and ReassemblySpeed shape the flight back home.
	ReassemblyEase  Easing
	ReassemblySpeed float64
	// FinalGradientStops colour the text once it lands. They are ignored when
	// the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
}

// DefaultUnstableConfig is upstream's default unstable.
func DefaultUnstableConfig() UnstableConfig {
	return UnstableConfig{
		UnstableColor:   MustParseColor("ff9200"),
		ExplosionEase:   OutExpo,
		ExplosionSpeed:  1,
		ReassemblyEase:  OutExpo,
		ReassemblySpeed: 1,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
	}
}

// The numbers upstream hard-codes rather than exposing as flags.
const (
	unstableGradientSteps  = 12
	unstableRumbleDuration = 10
	unstableFinalDuration  = 3
	unstableHoldTime       = 30
	unstableMaxRumbleSteps = 150
	unstableFirstShoveStep = 30
	unstableStartModDelay  = 18
)

// unstableGradientFrames is how many frames a twelve-step ramp between two
// stops produces: twelve, plus the exact end stop the gradient appends after
// each pair.
const unstableGradientFrames = unstableGradientSteps + 1

// unstableShoveOffsets are the three displacements a rumble frame picks from on
// each axis.
var unstableShoveOffsets = []int{-1, 0, 1}

type unstablePhase int

const (
	unstableRumble unstablePhase = iota
	unstableExplosion
	unstableReassembly
)

// Unstable scatters the characters into each other's places, shakes the whole
// screen while it heats up, throws every character off the nearest edge, and
// then flies them all back where they belong.
//
// It reassembles rather than sweeps, but nothing is hidden at any point: the
// characters are all on screen from the first frame, standing in the wrong
// places. That is upstream's own behaviour, so DynamicExistingColors needs no
// deviation for it here.
type Unstable struct {
	config UnstableConfig

	characters    []*Character
	jumbledCoords map[*Character]Coord

	phase              unstablePhase
	explosionHoldTime  int
	maxRumbleSteps     int
	currentRumbleSteps int
	rumbleModDelay     int

	// shoved says the last frame left every character one cell off its jumbled
	// position. The shove is undone at the top of the next Advance rather than
	// straight after the frame that made it: ttfx renders inside next_frame and
	// can put the characters back before returning, while here the host reads
	// the frame after Advance has returned, so undoing it any earlier would
	// render the rumble as a still picture.
	shoved bool

	// ticking is the snapshot Advance walks. Engine.ActiveCharacters reuses its
	// slice, so the copy is kept here rather than held across a tick.
	ticking []*Character
}

// NewUnstable builds the effect.
func NewUnstable(config UnstableConfig) *Unstable {
	return &Unstable{config: config, jumbledCoords: map[*Character]Coord{}}
}

// Build scrambles the characters into each other's coordinates, gives each one
// the flight out and the flight home, and starts the rumble.
func (u *Unstable) Build(e *Engine) error {
	finalGradient, err := NewGradient(u.config.FinalGradientStops, u.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		u.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	// ttfx re-runs this query on every rumble frame. The sort is deterministic
	// and the population never changes, so it runs once and is kept.
	u.characters = e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)

	startColors := make(map[*Character]ColorPair, len(u.characters))
	finalColors := make(map[*Character]ColorPair, len(u.characters))
	for _, ch := range u.characters {
		if dynamic {
			// The rumble leaves from the colour the character arrived wearing
			// and settles back onto it, background included.
			input := ch.Animation.InputColors
			start := input
			if !start.HasFg {
				start.Fg, start.HasFg = DynamicNeutralGrey, true
			}
			startColors[ch] = start
			finalColors[ch] = input
			continue
		}
		pair := Fg(mapping.At(ch.InputCoord, fallback))
		startColors[ch] = pair
		finalColors[ch] = pair
	}

	// Every character takes some other character's coordinate, drawn without
	// replacement, so the screen starts as a shuffle of itself.
	coordPool := make([]Coord, len(u.characters))
	for i, ch := range u.characters {
		coordPool[i] = ch.InputCoord
	}
	u.jumbledCoords = make(map[*Character]Coord, len(u.characters))

	for _, ch := range u.characters {
		target := u.explosionTarget(e)
		index := e.Rng.IntBetween(0, len(coordPool)-1)
		jumbled := coordPool[index]
		coordPool = append(coordPool[:index], coordPool[index+1:]...)
		u.jumbledCoords[ch] = jumbled
		ch.Motion.SetCoordinate(jumbled)

		explosion, err := ch.Motion.NewPath("explosion", PathOptions{
			Speed: u.config.ExplosionSpeed, Ease: u.config.ExplosionEase, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := explosion.NewWaypoint(target, nil, ""); err != nil {
			return err
		}
		reassembly, err := ch.Motion.NewPath("reassembly", PathOptions{
			Speed: u.config.ReassemblySpeed, Ease: u.config.ReassemblyEase, HasEase: true,
		})
		if err != nil {
			return err
		}
		if _, err := reassembly.NewWaypoint(ch.InputCoord, nil, ""); err != nil {
			return err
		}

		if err := u.buildRumbleScene(ch, startColors[ch]); err != nil {
			return err
		}
		if err := u.buildFinalScene(ch, finalColors[ch], dynamic); err != nil {
			return err
		}

		e.ActivateScene(ch, "rumble")
		if dynamic {
			// The character is about to be shown, so give it the colour it
			// arrived with rather than a bare symbol for one frame.
			ch.Animation.SetAppearance(ch.InputSymbol, startColors[ch], ch.UsesInputColors)
		}
		e.Terminal.SetCharacterVisibility(ch, true)
	}

	u.phase = unstableRumble
	u.explosionHoldTime = unstableHoldTime
	u.maxRumbleSteps = unstableMaxRumbleSteps
	u.currentRumbleSteps = 0
	u.rumbleModDelay = unstableStartModDelay
	u.shoved = false
	return nil
}

// explosionTarget picks the point on the canvas edge a character is thrown to.
func (u *Unstable) explosionTarget(e *Engine) Coord {
	canvas := e.Terminal.Canvas
	switch e.Rng.IntBetween(0, 3) {
	case 0:
		return C(canvas.Left, canvas.RandomRow(e.Rng, false))
	case 1:
		return C(canvas.Right, canvas.RandomRow(e.Rng, false))
	case 2:
		return C(canvas.RandomColumn(e.Rng, false), canvas.Bottom)
	default:
		return C(canvas.RandomColumn(e.Rng, false), canvas.Top)
	}
}

// buildRumbleScene ramps the character from the colour it starts on up to the
// unstable colour, which is what makes the screen look like it is heating up.
func (u *Unstable) buildRumbleScene(ch *Character, start ColorPair) error {
	rumble := ch.Animation.NewScene("rumble", SceneOptions{
		UsesInputColors: ch.UsesInputColors,
		Frames:          unstableGradientFrames,
	})
	startFg := DynamicNeutralGrey
	if start.HasFg {
		startFg = start.Fg
	}
	fg, err := NewGradientSteps([]Color{startFg, u.config.UnstableColor}, unstableGradientSteps, false)
	if err != nil {
		return err
	}
	var bg *Gradient
	if start.HasBg {
		// Only a character that arrived with a background gets here, so this is
		// the DynamicExistingColors path. The background has to heat up with
		// the foreground, or every filled panel and selection bar on a captured
		// screen blinks out for the length of the rumble.
		if bg, err = NewGradientSteps(
			[]Color{start.Bg, u.config.UnstableColor}, unstableGradientSteps, false); err != nil {
			return err
		}
	}
	return rumble.ApplyGradientToSymbols([]string{ch.InputSymbol}, unstableRumbleDuration, fg, bg)
}

// buildFinalScene cools the character back down from the unstable colour to the
// one it settles on while it flies home.
func (u *Unstable) buildFinalScene(ch *Character, final ColorPair, dynamic bool) error {
	scene := ch.Animation.NewScene("final", SceneOptions{
		UsesInputColors: ch.UsesInputColors,
		Frames:          unstableGradientFrames + 1,
	})
	if !dynamic {
		fgTarget := DynamicNeutralGrey
		if final.HasFg {
			fgTarget = final.Fg
		}
		fg, err := NewGradientSteps(
			[]Color{u.config.UnstableColor, fgTarget}, unstableGradientSteps, false)
		if err != nil {
			return err
		}
		return scene.ApplyGradientToSymbols([]string{ch.InputSymbol}, unstableFinalDuration, fg, nil)
	}

	if !final.HasFg && !final.HasBg {
		// The character carried no colours in, so it cools to grey and then
		// drops back to whatever the terminal itself is drawing in.
		fg, err := NewGradientSteps(
			[]Color{u.config.UnstableColor, DynamicNeutralGrey}, unstableGradientSteps, false)
		if err != nil {
			return err
		}
		if err := scene.ApplyGradientToSymbols(
			[]string{ch.InputSymbol}, unstableFinalDuration, fg, nil); err != nil {
			return err
		}
		return scene.AddFrame(ch.InputSymbol, unstableFinalDuration, VisualParams{})
	}

	var fg, bg *Gradient
	var err error
	if final.HasFg {
		if fg, err = NewGradientSteps(
			[]Color{u.config.UnstableColor, final.Fg}, unstableGradientSteps, false); err != nil {
			return err
		}
	}
	if final.HasBg {
		if bg, err = NewGradientSteps(
			[]Color{u.config.UnstableColor, final.Bg}, unstableGradientSteps, false); err != nil {
			return err
		}
	}
	if err := scene.ApplyGradientToSymbols(
		[]string{ch.InputSymbol}, unstableFinalDuration, fg, bg); err != nil {
		return err
	}
	if !final.HasFg {
		// A cell that carried only a background settles onto that background
		// with no foreground of its own, which a ramp cannot express.
		return scene.AddFrame(ch.InputSymbol, unstableFinalDuration, VisualParams{Colors: Bg(final.Bg)})
	}
	return nil
}

// Advance runs one frame and reports whether the effect is still going.
//
// It steps the characters itself rather than calling Engine.Update, as ttfx
// does: the rumble advances animation without motion, and both flights decide
// what stays active by where a character has got to rather than by whether it
// still has work. The clock therefore does not move during this effect, which
// is also ttfx's behaviour and costs nothing, since unstable never reads it.
func (u *Unstable) Advance(e *Engine) bool {
	u.undoShove()
	produced := false

	if u.phase == unstableRumble {
		if u.currentRumbleSteps < u.maxRumbleSteps {
			u.rumbleStep(e)
			produced = true
		} else {
			u.phase = unstableExplosion
			for _, ch := range u.characters {
				e.ActivatePath(ch, "explosion")
			}
			e.ClearActive()
			for _, ch := range u.characters {
				e.Activate(ch)
			}
		}
	}

	if u.phase == unstableExplosion {
		switch {
		case e.ActiveCount() > 0:
			u.tickActive(e, "explosion", false)
			produced = true
		case u.explosionHoldTime != 0:
			// The screen sits empty for a moment before the characters come
			// back. Upstream ticks the empty active set here, which does
			// nothing.
			u.explosionHoldTime--
			produced = true
		default:
			u.phase = unstableReassembly
			for _, ch := range u.characters {
				e.ActivateScene(ch, "final")
				e.Activate(ch)
				e.ActivatePath(ch, "reassembly")
			}
		}
	}

	if u.phase == unstableReassembly && e.ActiveCount() > 0 {
		u.tickActive(e, "reassembly", true)
		produced = true
	}

	return produced
}

// rumbleStep advances the rumble by one frame, shoving the whole screen a cell
// off true every so often and shortening the gap between shoves each time.
func (u *Unstable) rumbleStep(e *Engine) {
	shove := u.currentRumbleSteps > unstableFirstShoveStep &&
		u.currentRumbleSteps%u.rumbleModDelay == 0
	if shove {
		rowOffset := *Choice(e.Rng, unstableShoveOffsets)
		columnOffset := *Choice(e.Rng, unstableShoveOffsets)
		for _, ch := range u.characters {
			at := ch.Motion.CurrentCoord
			ch.Motion.SetCoordinate(C(at.Column+columnOffset, at.Row+rowOffset))
			e.StepAnimation(ch)
		}
		u.shoved = true
		u.rumbleModDelay = max(u.rumbleModDelay-1, 1)
	} else {
		for _, ch := range u.characters {
			e.StepAnimation(ch)
		}
	}
	u.currentRumbleSteps++
}

// undoShove puts every character back on its jumbled coordinate after a shoved
// frame has been read. See Unstable.shoved.
func (u *Unstable) undoShove() {
	if !u.shoved {
		return
	}
	for _, ch := range u.characters {
		ch.Motion.SetCoordinate(u.jumbledCoords[ch])
	}
	u.shoved = false
}

// tickActive advances every active character and drops the ones that have
// arrived at the named path's waypoint, optionally waiting for the animation to
// finish as well.
//
// ttfx re-reads the active set to filter it. Nothing here registers an event
// handler, so the set cannot change during the ticks and the snapshot taken for
// them is filtered directly.
func (u *Unstable) tickActive(e *Engine, pathID string, waitForScene bool) {
	u.ticking = append(u.ticking[:0], e.ActiveCharacters()...)
	for _, ch := range u.ticking {
		e.Tick(ch)
	}
	for _, ch := range u.ticking {
		path := ch.Motion.Path(pathID)
		if path == nil || len(path.Waypoints) == 0 {
			continue
		}
		if ch.Motion.CurrentCoord != path.Waypoints[0].Coord {
			continue
		}
		if waitForScene && !ch.Animation.ActiveSceneIsComplete() {
			continue
		}
		e.Deactivate(ch)
	}
}
