package tuiffects

import "fmt"

// laseretch, ported from ttfx src/effects/laseretch.rs, which ports
// TerminalTextEffects effects/effect_laseretch.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "laseretch",
		Description: "A laser beam cuts the text onto the screen one character at a time, throwing sparks",
		New:         func() Effect { return NewLaserEtch(DefaultLaserEtchConfig()) },
		// The etch order comes from a recursive backtracker run over the
		// whole text block, so the empty cells inside it need characters
		// too. Without them the walk starts on nothing and Build fails.
		NeedsFillCharacters: true,
	})
}

// EtchPattern picks the order the laser etches characters in.
type EtchPattern int

// The etch patterns.
const (
	// EtchAlgorithm cuts along a recursive backtracker's walk over the text
	// block, which gives the laser long corridors and dead ends rather than a
	// tidy sweep. This is the default and the only pattern that etches.
	EtchAlgorithm EtchPattern = iota
	// EtchGroup is upstream's grouped ordering, and it etches nothing.
	//
	// Upstream parses the option into a CharacterGroup member and then tests
	// that member against the enum's member *names*, which never matches. The
	// grouped branch is unreachable, no character is ever queued, and the
	// effect emits a single frame. ttfx reproduced that after checking it
	// against the reference build, and so does this.
	//
	// ttfx carries the requested group alongside the variant. Nothing reads
	// it, so this does not: the outcome is the same whichever group is asked
	// for.
	EtchGroup
)

// LaserEtchConfig tunes the laseretch effect.
type LaserEtchConfig struct {
	// EtchPattern is the order characters are etched in. See EtchPattern:
	// only EtchAlgorithm etches.
	EtchPattern EtchPattern
	// EtchSpeed is how many characters are etched in one go, and EtchDelay is
	// how many frames pass before the next go. Together they set the pace.
	EtchSpeed int
	EtchDelay int
	// CoolGradientStops colour a character as it cools from laser-hot back to
	// its settled colour.
	CoolGradientStops []Color
	// LaserGradientStops colour the beam itself. The gradient loops and each
	// cell of the beam starts one step further along it, so the colour runs
	// up the beam.
	LaserGradientStops []Color
	// SparkGradientStops colour a spark as it cools on the way down, and
	// SparkCoolingFrames is how long each of those colours is held. Raise it
	// to cool sparks more slowly.
	SparkGradientStops []Color
	SparkCoolingFrames int
	// FinalGradientStops colour the text once it has cooled. They are ignored
	// when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
	// FinalGradientFrames is carried for parity with upstream's options and
	// changes nothing. Upstream declares it and never reads it: laseretch has
	// no separate final-gradient scene, because the cool gradient already
	// ends on the final colour.
	FinalGradientFrames int
}

// DefaultLaserEtchConfig is upstream's default laseretch.
func DefaultLaserEtchConfig() LaserEtchConfig {
	return LaserEtchConfig{
		EtchPattern:        EtchAlgorithm,
		EtchSpeed:          1,
		EtchDelay:          1,
		CoolGradientStops:  []Color{MustParseColor("ffe680"), MustParseColor("ff7b00")},
		LaserGradientStops: []Color{MustParseColor("ffffff"), MustParseColor("376cff")},
		SparkGradientStops: []Color{
			MustParseColor("ffffff"), MustParseColor("ffe680"),
			MustParseColor("ff7b00"), MustParseColor("1a0900"),
		},
		SparkCoolingFrames: 7,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("ffffff"),
		},
		FinalGradientSteps:     []int{8},
		FinalGradientDirection: Vertical,
		FinalGradientFrames:    4,
	}
}

// laserBeamFrameDuration is how long each colour of the beam is held.
const laserBeamFrameDuration = 3

// laserEtchFrameDuration is how long each colour of a character's cooling
// ramp is held.
const laserEtchFrameDuration = 3

// laserSparkPoolSize is how many sparks are made up front. Upstream picks the
// same number and never grows past it in practice: a spark lives about a
// hundred frames and only one is thrown per etched character.
const laserSparkPoolSize = 2000

// laserSparkSymbols are the glyphs a spark can be drawn with.
var laserSparkSymbols = []string{".", ",", "*"}

// laser is the beam: where it is pointing, the diagonal line of characters
// that draws it, and the pool of sparks it throws off.
type laser struct {
	position  Coord
	beamChars []*Character
	sparks    *ParticlePool
}

// LaserEtch cuts the text onto the screen. A beam swings in from off the
// bottom left corner, stops on one character at a time in the order a
// recursive backtracker walked the text block, and leaves that character
// glowing and cooling behind it. Every stop throws a spark that falls to the
// bottom of the canvas and burns out.
type LaserEtch struct {
	config       LaserEtchConfig
	pendingChars []*Character
	charDelay    int
	laser        *laser
}

// NewLaserEtch builds the effect.
func NewLaserEtch(config LaserEtchConfig) *LaserEtch {
	return &LaserEtch{config: config}
}

// laserHasInputColors is upstream's _has_input_colors: whether the character
// arrived carrying a colour of its own. A blank cell that carried one is still
// worth etching, so it is not skipped as whitespace.
func laserHasInputColors(ch *Character) bool {
	return ch.Animation.InputColors.HasFg || ch.Animation.InputColors.HasBg
}

// Build gives every character the scene it wears while it cools, works out the
// order the laser will etch in, then builds the beam.
func (l *LaserEtch) Build(e *Engine) error {
	// ttfx validates these through clap value parsers before the effect is
	// built. There is no command line here, so the check moves to Build.
	if l.config.EtchSpeed < 1 {
		return fmt.Errorf("laseretch: etch speed must be at least 1, got %d", l.config.EtchSpeed)
	}
	if l.config.EtchDelay < 0 {
		return fmt.Errorf("laseretch: etch delay must not be negative, got %d", l.config.EtchDelay)
	}
	if l.config.SparkCoolingFrames < 1 {
		return fmt.Errorf("laseretch: spark cooling frames must be at least 1, got %d", l.config.SparkCoolingFrames)
	}

	finalGradient, err := NewGradient(l.config.FinalGradientStops, l.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		l.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]
	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors

	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		final := Fg(mapping.At(ch.InputCoord, fallback))
		coolStops := l.config.CoolGradientStops
		if dynamic {
			// The picture is already on the screen, so a character settles
			// back to the colour it arrived with rather than to the effect's
			// own gradient, and the cooling ramp does not end on that colour.
			// The ramp from the last cool colour to the input colour is added
			// after it instead.
			final = ch.Animation.InputColors
		} else {
			coolStops = append(append([]Color(nil), coolStops...), final.Fg)
		}
		cool, err := NewGradientSteps(coolStops, 8, false)
		if err != nil {
			return err
		}
		coolLast := cool.Spectrum[len(cool.Spectrum)-1]

		frames := 1 + len(cool.Spectrum)
		if dynamic {
			frames += 9
		}
		spawn := ch.Animation.NewScene("spawn", SceneOptions{
			UsesInputColors: ch.UsesInputColors,
			Frames:          frames,
		})
		if err := spawn.AddFrame("^", laserEtchFrameDuration, VisualParams{
			Colors: Fg(MustParseColor("ffe680")),
		}); err != nil {
			return err
		}
		for _, color := range cool.Spectrum {
			if err := spawn.AddFrame(ch.InputSymbol, laserEtchFrameDuration, VisualParams{
				Colors: Fg(color),
			}); err != nil {
				return err
			}
		}
		if dynamic {
			// Both ramps start from the last cool colour, which is where the
			// character actually is when they begin. The background gets its
			// own ramp so a captured selection bar or filled panel comes back
			// rather than staying blank for the rest of the run.
			if final.HasFg || final.HasBg {
				var fgGradient, bgGradient *Gradient
				if final.HasFg {
					if fgGradient, err = NewGradientSteps([]Color{coolLast, final.Fg}, 8, false); err != nil {
						return err
					}
				}
				if final.HasBg {
					if bgGradient, err = NewGradientSteps([]Color{coolLast, final.Bg}, 8, false); err != nil {
						return err
					}
				}
				if err := spawn.ApplyGradientToSymbols(
					[]string{ch.InputSymbol}, laserEtchFrameDuration, fgGradient, bgGradient); err != nil {
					return err
				}
			} else {
				// A character that carried no colour of its own cools to
				// white and then drops its colouring altogether, so it ends
				// up wearing whatever the terminal's own default is.
				whiteCooldown, err := NewGradientSteps(
					[]Color{coolLast, MustParseColor("ffffff")}, 8, false)
				if err != nil {
					return err
				}
				if err := spawn.ApplyGradientToSymbols(
					[]string{ch.InputSymbol}, laserEtchFrameDuration, whiteCooldown, nil); err != nil {
					return err
				}
				if err := spawn.AddFrame(
					ch.InputSymbol, laserEtchFrameDuration, VisualParams{}); err != nil {
					return err
				}
			}
		}
		e.ActivateScene(ch, spawn.ID)
	}

	// Every character is left hidden, in both colour modes. Laseretch
	// assembles the screen rather than passing over it: the laser is what
	// puts a character on the canvas, so showing them up front would hand the
	// finished picture over before the beam had cut a single cell.
	if l.config.EtchPattern == EtchAlgorithm {
		walk, err := NewRecursiveBacktracker(e, nil, true)
		if err != nil {
			return err
		}
		for !walk.Complete {
			walk.Step(e)
		}
		l.pendingChars = walk.CharLinkOrder
	}

	l.charDelay = 0
	beam, err := l.makeLaser(e)
	if err != nil {
		return err
	}
	l.laser = beam
	for _, ch := range beam.beamChars {
		e.Activate(ch)
	}
	return nil
}

// makeLaser builds the spark pool and then the beam, in that order, because
// upstream allocates its characters in that order and the pool's random symbol
// choices come off the same stream.
func (l *LaserEtch) makeLaser(e *Engine) (*laser, error) {
	laserGradient, err := NewGradient(l.config.LaserGradientStops, []int{6}, true)
	if err != nil {
		return nil, err
	}
	sparkGradient, err := NewGradient(l.config.SparkGradientStops, []int{3, 8}, false)
	if err != nil {
		return nil, err
	}

	// The pool holds the initializer, so unlike ttfx it is written once here
	// rather than passed to preallocate and to every emission.
	sparkColors := sparkGradient.Spectrum
	coolingFrames := l.config.SparkCoolingFrames
	sparks, err := NewParticlePool(laserSparkSymbols, 0, Coord{}, func(e *Engine, spark *Character) {
		spark.Layer = 2
		scene := spark.Animation.NewScene("spark", SceneOptions{
			UsesInputColors: spark.UsesInputColors,
			Frames:          len(sparkColors),
		})
		for _, color := range sparkColors {
			// coolingFrames is checked in Build, so AddFrame cannot fail here.
			_ = scene.AddFrame(spark.InputSymbol, coolingFrames, VisualParams{Colors: Fg(color)})
		}
	})
	if err != nil {
		return nil, err
	}
	if err := sparks.Preallocate(e, laserSparkPoolSize); err != nil {
		return nil, err
	}
	for _, spark := range sparks.Particles {
		sparks.ReclaimOnEvent(spark, SceneComplete, SceneCaller("spark"), true, true)
	}

	// The beam is a diagonal of characters running up and to the right from
	// wherever it is pointing. It starts off the bottom left corner of the
	// canvas, at row zero, which is one below the bottom row.
	beamColors := append([]Color(nil), laserGradient.Spectrum...)
	beamChars := make([]*Character, 0, e.Terminal.Canvas.Top+1)
	for row, column := 0, 0; row <= e.Terminal.Canvas.Top; row, column = row+1, column+1 {
		symbol := "/"
		if len(beamChars) == 0 {
			symbol = "*"
		}
		ch := e.Terminal.AddCharacter(symbol, C(column, row))
		ch.Layer = 2
		e.Terminal.SetCharacterVisibility(ch, true)
		beamChars = append(beamChars, ch)

		scene := ch.Animation.NewScene("laser", SceneOptions{
			Looping:         true,
			UsesInputColors: ch.UsesInputColors,
			Frames:          len(beamColors),
		})
		for _, color := range beamColors {
			if err := scene.AddFrame(ch.InputSymbol, laserBeamFrameDuration, VisualParams{
				Colors: Fg(color),
			}); err != nil {
				return nil, err
			}
		}
		// Upstream rotates a deque by one per beam cell, so each cell starts
		// one colour further along and the gradient appears to run up the
		// beam.
		if len(beamColors) > 1 {
			first := beamColors[0]
			copy(beamColors, beamColors[1:])
			beamColors[len(beamColors)-1] = first
		}
		e.ActivateScene(ch, scene.ID)
	}

	return &laser{position: C(0, 0), beamChars: beamChars, sparks: sparks}, nil
}

// reposition points the beam at a coordinate and throws one spark from it.
func (l *LaserEtch) reposition(e *Engine, target Coord) {
	l.laser.position = target
	row, column := target.Row, target.Column
	for _, ch := range l.laser.beamChars {
		ch.Motion.SetCoordinate(C(column, row))
		row++
		column++
	}
	l.emitSparks(e, 1)
}

// emitSparks throws sparks off the point the beam is resting on. Each one gets
// a fresh curved path down to the bottom of the canvas for this flight; the
// scene it cools through was built once, when the pool made it.
func (l *LaserEtch) emitSparks(e *Engine, count int) {
	position := l.laser.position
	bottom := e.Terminal.Canvas.Bottom
	for i := 0; i < count; i++ {
		l.laser.sparks.Emit(e, position, "", true, ParticleReset{}, func(e *Engine, spark *Character) {
			path, err := spark.Motion.NewPath("", PathOptions{
				Speed: 0.3, Ease: OutSine, HasEase: true,
			})
			if err != nil {
				return
			}
			fallTarget := C(e.Rng.IntBetween(position.Column-20, position.Column+20), bottom)
			control := C(fallTarget.Column, position.Row+e.Rng.IntBetween(-10, 20))
			if _, err := path.NewWaypoint(fallTarget, []Coord{control}, ""); err != nil {
				return
			}
			e.ActivatePath(spark, path.ID)
			e.ActivateScene(spark, "spark")
		})
	}
}

// disable switches the beam off once there is nothing left to etch.
func (l *LaserEtch) disable(e *Engine) {
	for _, ch := range l.laser.beamChars {
		e.Terminal.SetCharacterVisibility(ch, false)
	}
}

// Advance etches the next characters, keeps the beam alive while any remain,
// and reports whether the effect is still going.
func (l *LaserEtch) Advance(e *Engine) bool {
	if len(l.pendingChars) == 0 && e.ActiveCount() == 0 {
		return false
	}
	if l.charDelay == 0 {
		for i := 0; i < l.config.EtchSpeed; i++ {
			if len(l.pendingChars) == 0 {
				break
			}
			next := l.pendingChars[0]
			l.pendingChars = l.pendingChars[1:]
			// The walk covers every cell of the text block, so it turns up
			// blanks with nothing to show. They are dropped and the next
			// character is taken instead. A blank that arrived carrying its
			// own colour is not a gap and is etched like any other cell.
			//
			// Upstream leaves the last one etched anyway when the queue runs
			// out mid-skip. Kept.
			for next.InputSymbol == " " && !laserHasInputColors(next) {
				if len(l.pendingChars) == 0 {
					break
				}
				next = l.pendingChars[0]
				l.pendingChars = l.pendingChars[1:]
			}
			e.Terminal.SetCharacterVisibility(next, true)
			e.Activate(next)
			l.reposition(e, next.InputCoord)
		}
		l.charDelay = l.config.EtchDelay
	} else {
		l.charDelay--
	}
	if len(l.pendingChars) != 0 {
		// The beam's scene loops, and a looping scene reads as complete, so
		// Update drops the beam cells out of the active set every frame. They
		// are put back until there is nothing left to etch.
		for _, ch := range l.laser.beamChars {
			e.Activate(ch)
		}
	} else {
		l.disable(e)
	}
	e.Update()
	// The beam and the sparks carry a foreground and no background, so over a
	// captured screen they take the fill out of whatever they cross for as
	// long as they are over it, and the beam crosses the whole height of the
	// canvas every time it moves.
	carryAddedCharactersOverBackgrounds(e)
	return true
}
