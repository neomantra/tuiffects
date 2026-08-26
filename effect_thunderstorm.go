package tuiffects

import "math"

// thunderstorm, ported from ttfx src/effects/thunderstorm.rs, which ports
// TerminalTextEffects effects/effect_thunderstorm.py by ChrisBuilds.

func init() {
	Register(Descriptor{
		Name:        "thunderstorm",
		Description: "The text dims, rain falls across it, and lightning strikes light it up before the sky clears",
		New:         func() Effect { return NewThunderstorm(DefaultThunderstormConfig()) },
	})
}

// ThunderstormConfig tunes the thunderstorm effect.
type ThunderstormConfig struct {
	// LightningColor is what a strike is drawn in.
	LightningColor Color
	// GlowingTextColor is the colour a character is left glowing after a
	// strike has passed through its cell.
	GlowingTextColor Color
	// TextGlowTime is how long each colour of that glow is held. Raise it to
	// cool the text more slowly.
	TextGlowTime int
	// RaindropSymbols are what a raindrop can look like.
	RaindropSymbols []string
	// SparkSymbols are what a spark thrown off by an impact can look like.
	SparkSymbols []string
	// SparkGlowColor is the colour a spark starts at before it cools.
	SparkGlowColor Color
	// SparkGlowTime is how long each colour of a spark's cooling is held.
	SparkGlowTime int
	// StormTime is how long the storm lasts, in seconds. The engine's clock
	// counts frames rather than the machine, so this is seconds of animation
	// at the rate the host says it paints at; see clock.go.
	StormTime int
	// FinalGradientStops colour the text once the sky has cleared. They are
	// ignored when the engine is set to resolve to the input's own colours.
	FinalGradientStops     []Color
	FinalGradientSteps     []int
	FinalGradientDirection GradientDirection
	// FinalGradientFrames is carried for parity with upstream's options and
	// changes nothing. Upstream declares it and never reads it: thunderstorm
	// has no separate final-gradient scene, because the text is already
	// wearing its final colour before the storm starts and only dims away
	// from it and back.
	FinalGradientFrames int
}

// DefaultThunderstormConfig is upstream's default thunderstorm.
func DefaultThunderstormConfig() ThunderstormConfig {
	return ThunderstormConfig{
		LightningColor:   MustParseColor("68A3E8"),
		GlowingTextColor: MustParseColor("EF5411"),
		TextGlowTime:     6,
		RaindropSymbols:  []string{"\\", ".", ","},
		SparkSymbols:     []string{"*", ".", "'"},
		SparkGlowColor:   MustParseColor("ff4d00"),
		SparkGlowTime:    18,
		StormTime:        12,
		FinalGradientStops: []Color{
			MustParseColor("8A008A"), MustParseColor("00D1FF"), MustParseColor("FFFFFF"),
		},
		FinalGradientSteps:     []int{12},
		FinalGradientDirection: Vertical,
		FinalGradientFrames:    3,
	}
}

// thunderstormBackground is the colour a strike fades out into and a spark
// cools down to.
//
// ttfx reads this from terminal_background_color, a terminal config field this
// engine does not have. The value here is ttfx's own default, so the effect
// matches upstream for every host that never changed it, which is every host
// here. randomsequence carries the same constant for the same reason.
var thunderstormBackground = MustParseColor("000000")

// thunderstormNeutralGray is the colour a character dims away from when the
// input gave it no foreground of its own and the engine is resolving to input
// colours. Without it such a character would have nothing to ramp.
var thunderstormNeutralGray = MustParseColor("808080")

// thunderstormRaindropColor is the pale blue every raindrop is drawn in.
var thunderstormRaindropColor = MustParseColor("aaaaff")

// The shapes a strike can take. A strike char is created carrying "|", so the
// two arm lists are only ever consulted through a branch neighbour whose
// symbol upstream can never actually set; see setupLightningStrike.
var (
	thunderstormStrikeSymbols = []string{"\\", "/", "|"}
	thunderstormRightArms     = []string{"|", "\\"}
	thunderstormLeftArms      = []string{"|", "/"}
	thunderstormStep          = []int{-1, 1}
	thunderstormSparkSide     = []int{1, -1}
)

const (
	// thunderstormStrikeChance is the chance, per frame, that a storm with no
	// strike in flight starts one.
	thunderstormStrikeChance = 0.008
	// thunderstormBranchChance is the chance that a cell of a strike throws
	// off a fork. It drops by a hundredth for each fork already taken and is
	// put back at the end of every strike.
	thunderstormBranchChance = 0.05
	thunderstormBranchDecay  = 0.01

	// thunderstormStrikeBatch is how many strike characters are made at once
	// when the pool of them runs dry mid-strike.
	thunderstormStrikeBatch = 20
	// thunderstormStrikePool is how many are made up front.
	thunderstormStrikePool = 200

	// thunderstormRaindropCount and thunderstormSparkCount are how many
	// particles of each kind are made before the first frame.
	thunderstormRaindropCount = 50
	thunderstormSparkCount    = 200

	// thunderstormGradientSteps is the step count every ramp in this effect
	// is built with, apart from a strike's own fade.
	thunderstormGradientSteps = 7
	// thunderstormFadeSteps is the step count of a strike's fade to the
	// terminal background.
	thunderstormFadeSteps = 6

	// The frame durations upstream holds each ramp's colours for.
	thunderstormFlashFrameDuration = 6
	thunderstormStrikeFadeDuration = 2
	thunderstormTextFadeDuration   = 12
)

// thunderstormPhase is where the effect has got to. Upstream keeps this as a
// string on the iterator.
type thunderstormPhase int

const (
	// thunderstormPreStorm has done nothing yet. The next frame starts the
	// text dimming.
	thunderstormPreStorm thunderstormPhase = iota
	// thunderstormWaiting is dimming, and does nothing until the reference
	// character reports the dimming finished.
	thunderstormWaiting
	// thunderstormStorm is raining, and striking now and then.
	thunderstormStorm
	// thunderstormComplete has started the text brightening back up. The
	// effect ends once nothing is left animating.
	thunderstormComplete
)

// thunderstormText is one input character and the colours its lightning flash
// runs through, kept so the flash can be rebuilt for each strike. See
// applyFlashEase for why it has to be rebuilt rather than retimed.
type thunderstormText struct {
	ch    *Character
	flash []ColorPair
}

// Thunderstorm dims the text, rains on it, and strikes it with lightning.
//
// The text is on screen the whole time: it fades down to a dim version of
// itself, weather happens over the top of it, and it fades back up at the end.
// A strike is drawn as a column of added characters that walks down from the
// top of the canvas one small batch a frame, forking now and then. When it
// lands, every character on the screen flashes, the cells the strike passed
// through are left glowing, and a handful of sparks fly out of the point of
// impact and cool as they go.
//
// This effect passes over the screen rather than assembling it. Every
// character is visible and wearing the colour it will settle back to from the
// first frame, in every colour mode, because there is nothing here that
// reveals the text: the storm plays over a picture that is already there. So
// there is no reveal to defer and no waves-style pre-show to add under
// DynamicExistingColors, and upstream needs no deviation for it. Backgrounds
// need none either: ttfx already carries the input background through the
// dim, the flash and the glow, so a selection bar or a filled panel on a
// captured screen dims and brightens with everything else instead of blinking
// out.
type Thunderstorm struct {
	config ThunderstormConfig

	// delay counts down the frames between one burst of rain and the next.
	delay int
	// strikeProgressionDelay counts down the frames between one batch of a
	// strike's cells appearing and the next.
	strikeProgressionDelay int

	// rain and sparks recycle the two kinds of particle. A run emits far more
	// of either than the pool holds, so they go round rather than being made
	// afresh.
	rain   *ParticlePool
	sparks *ParticlePool
	// sparkGradient is the cooling a spark runs through, built once and read
	// by the pool's initializer.
	sparkGradient *Gradient

	// The three lists a strike's characters move between: waiting to appear,
	// free to be used by the next strike, and on screen but not yet flashed.
	pendingStrikeChars   []*Character
	availableStrikeChars []*Character
	activeStrikeChars    []*Character

	// pendingGlowChars are the characters a strike has just lit up. They are
	// activated at the end of the frame that lit them, which is upstream's
	// way round the set being written while it is walked.
	pendingGlowChars []*Character

	strikeInProgress   bool
	strikeBranchChance float64

	phase          thunderstormPhase
	stormStartTime float64

	// text is every input character with the colours of its flash, so a
	// strike can rebuild the flash scene it needs.
	text []thunderstormText
}

// NewThunderstorm builds the effect.
func NewThunderstorm(config ThunderstormConfig) *Thunderstorm {
	return &Thunderstorm{config: config, strikeBranchChance: thunderstormBranchChance}
}

// thunderstormBezierEase is the easing curve a lightning flash runs on:
// upstream's bezier_easing, Newton-Raphson on x with its constants, twenty
// iterations, converging at a hundred-thousandth and bailing out on a flat
// derivative.
//
// This engine's Easing is a plain enumeration of the thirty-one named curves,
// and upstream's flash is the one place in the whole catalogue that asks for a
// parameterised curve, so the curve is evaluated here rather than added to the
// engine for a single caller. See applyFlashEase for how a scene is given it.
func thunderstormBezierEase(x1, y1, x2, y2, progress float64) float64 {
	sampleX := func(t float64) float64 {
		return 3*x1*(1-t)*(1-t)*t + 3*x2*(1-t)*t*t + t*t*t
	}
	sampleY := func(t float64) float64 {
		return 3*y1*(1-t)*(1-t)*t + 3*y2*(1-t)*t*t + t*t*t
	}
	derivativeX := func(t float64) float64 {
		return 3*(1-t)*(1-t)*x1 + 6*(1-t)*t*(x2-x1) + 3*t*t*(1-x2)
	}
	if progress <= 0 {
		return 0
	}
	if progress >= 1 {
		return 1
	}
	t := progress
	for i := 0; i < 20; i++ {
		delta := sampleX(t) - progress
		if math.Abs(delta) < 1e-5 {
			break
		}
		d := derivativeX(t)
		if math.Abs(d) < 1e-6 {
			break
		}
		t -= delta / d
	}
	return sampleY(t)
}

// thunderstormEasedRun is one stretch of an eased scene: the colour it shows
// and how many ticks it stays on it.
type thunderstormEasedRun struct {
	color int
	ticks int
}

// thunderstormEaseSchedule works out what an eased scene would show, tick for
// tick, so that a scene which cannot be told to ease can be built to play the
// same thing.
//
// An eased scene keeps all of its frames and picks one per tick off the curve;
// a plain scene plays them in order. Both run for the same number of ticks, so
// walking the curve here and holding each colour for as long as the curve
// stays on it gives the identical sequence of visuals, the identical total
// length and the identical SceneComplete. Runs are collapsed, so a fifteen
// colour ramp usually costs no more frames than it started with.
//
// The schedule depends on the curve and the shape of the ramp and not on the
// character, so a strike works one out and every character on the screen is
// built from it.
//
// Every colour is held for the same duration, which is what lets a tick map
// onto a colour by division. That is true of every scene this effect eases.
func thunderstormEaseSchedule(colors, duration int, ease func(float64) float64) []thunderstormEasedRun {
	total := colors * duration
	if total == 0 {
		return nil
	}
	final := total - 1
	var schedule []thunderstormEasedRun
	for tick := 0; tick < total; tick++ {
		index := roundHalfEven(ease(float64(tick)/float64(total)) * float64(final))
		index = min(max(index, 0), final)
		color := index / duration
		if n := len(schedule); n != 0 && schedule[n-1].color == color {
			schedule[n-1].ticks++
			continue
		}
		schedule = append(schedule, thunderstormEasedRun{color: color, ticks: 1})
	}
	return schedule
}

// thunderstormAddEasedFrames fills a scene from a schedule
// thunderstormEaseSchedule worked out.
func thunderstormAddEasedFrames(scene *Scene, symbol string, colors []ColorPair, schedule []thunderstormEasedRun) error {
	for _, run := range schedule {
		if err := scene.AddFrame(symbol, run.ticks, VisualParams{Colors: colors[run.color]}); err != nil {
			return err
		}
	}
	return nil
}

// thunderstormOptionalColor is one side of a ColorPair while a ramp is being
// built, which upstream carries as an Option and this engine carries as a
// colour with a flag beside it.
type thunderstormOptionalColor struct {
	color Color
	has   bool
}

// thunderstormChannelSteps is one channel of upstream's
// _add_color_pair_gradient_frames: a ramp when both ends have a colour, and
// the colour that is there repeated when only one of them does.
//
// Faithful quirk: with both ends present the gradient holds steps+1 entries
// and only the first steps of them are ever used, so the ramp never reaches
// its end stop. The frame that follows the ramp is what lands on it.
func thunderstormChannelSteps(start, end thunderstormOptionalColor, steps int) ([]thunderstormOptionalColor, error) {
	out := make([]thunderstormOptionalColor, steps)
	if start.has && end.has {
		gradient, err := NewGradientSteps([]Color{start.color, end.color}, steps, false)
		if err != nil {
			return nil, err
		}
		for i := 0; i < steps; i++ {
			out[i] = thunderstormOptionalColor{color: gradient.Spectrum[i], has: true}
		}
		return out, nil
	}
	filler := start
	if end.has {
		filler = end
	}
	for i := range out {
		out[i] = filler
	}
	return out, nil
}

// thunderstormPairColors zips two channels back into a pair.
func thunderstormPairColors(fg, bg thunderstormOptionalColor) ColorPair {
	var colors ColorPair
	if fg.has {
		colors.Fg, colors.HasFg = fg.color, true
	}
	if bg.has {
		colors.Bg, colors.HasBg = bg.color, true
	}
	return colors
}

// thunderstormPairGradientFrames ramps both channels of a pair at once, which
// is how a cell that arrived with a background dims and brightens without ever
// losing it. Upstream's _add_color_pair_gradient_frames.
func thunderstormPairGradientFrames(scene *Scene, symbol string, start, end ColorPair, steps, duration int) error {
	fgSteps, err := thunderstormChannelSteps(
		thunderstormOptionalColor{start.Fg, start.HasFg}, thunderstormOptionalColor{end.Fg, end.HasFg}, steps)
	if err != nil {
		return err
	}
	bgSteps, err := thunderstormChannelSteps(
		thunderstormOptionalColor{start.Bg, start.HasBg}, thunderstormOptionalColor{end.Bg, end.HasBg}, steps)
	if err != nil {
		return err
	}
	for i := 0; i < steps; i++ {
		if err := scene.AddFrame(symbol, duration,
			VisualParams{Colors: thunderstormPairColors(fgSteps[i], bgSteps[i])}); err != nil {
			return err
		}
	}
	return nil
}

// thunderstormAdjustPair dims or brightens both channels of a pair. Upstream's
// _adjust_color_pair_brightness.
func thunderstormAdjustPair(colors ColorPair, brightness float64) ColorPair {
	out := colors
	if colors.HasFg {
		out.Fg = AdjustColorBrightness(colors.Fg, brightness)
	}
	if colors.HasBg {
		out.Bg = AdjustColorBrightness(colors.Bg, brightness)
	}
	return out
}

// thunderstormInitRaindrop is run once on each raindrop the pool creates: the
// layer that puts it in front of the text and the pale blue it falls in. A
// raindrop has no scene, only a path.
func thunderstormInitRaindrop(e *Engine, ch *Character) {
	ch.Layer = 1
	ch.Animation.SetAppearance(ch.InputSymbol, Fg(thunderstormRaindropColor), ch.UsesInputColors)
}

// initializeSpark is run once on each spark the pool creates: the layer that
// puts it in front of everything, and the one scene it uses for its whole
// life, cooling from the glow colour down to the terminal background.
func (t *Thunderstorm) initializeSpark(e *Engine, ch *Character) {
	ch.Layer = 2
	scene := ch.Animation.NewScene("glow", SceneOptions{
		Ease:            InCirc,
		HasEase:         true,
		UsesInputColors: ch.UsesInputColors,
		Frames:          len(t.sparkGradient.Spectrum),
	})
	for _, color := range t.sparkGradient.Spectrum {
		// AddFrame only rejects a duration below one, and the config parser
		// keeps SparkGlowTime above zero, so this cannot fail. The pool's
		// initializer has no error to report it through in any case.
		_ = scene.AddFrame(ch.InputSymbol, t.config.SparkGlowTime, VisualParams{Colors: Fg(color)})
	}
}

// setupRaindrop gives a raindrop the fall it is being emitted for: a diagonal
// down and to the right, from wherever it was spawned to one row below the
// canvas floor, at a speed of its own. It reclaims itself when it lands.
func (t *Thunderstorm) setupRaindrop(e *Engine, ch *Character) {
	origin := ch.Motion.CurrentCoord
	canvas := e.Terminal.Canvas
	path, err := ch.Motion.NewPath("", PathOptions{Speed: e.Rng.Uniform(0.5, 1.5)})
	if err != nil {
		return
	}
	if _, err := path.NewWaypoint(C(origin.Column+canvas.Top+1, canvas.Bottom-1), nil, ""); err != nil {
		return
	}
	// The emission cleared this drop's events, so the registration is made
	// again for each flight rather than once for the run.
	t.rain.ReclaimOnEvent(ch, PathComplete, PathCaller(path.ID), true, true)
	e.ActivatePath(ch, path.ID)
}

// setupSparksForImpact gives a spark the flight it is being emitted for: a
// slow curve out to one side of the impact and down to the canvas floor, where
// it hangs while it finishes cooling. It reclaims itself when it has cooled.
func (t *Thunderstorm) setupSparksForImpact(e *Engine, ch *Character) {
	impact := ch.Motion.CurrentCoord
	canvas := e.Terminal.Canvas
	path, err := ch.Motion.NewPath("", PathOptions{
		Speed:    e.Rng.Uniform(0.1, 0.25),
		Ease:     OutQuint,
		HasEase:  true,
		HoldTime: 30,
	})
	if err != nil {
		return
	}
	offset := e.Rng.IntBetween(4, 20) * *Choice(e.Rng, thunderstormSparkSide)
	target := C(impact.Column+offset, canvas.Bottom)
	control := C(impact.Column-floorDiv(impact.Column-target.Column, 2), e.Rng.IntBetween(1, canvas.Top))
	if _, err := path.NewWaypoint(target, []Coord{control}, ""); err != nil {
		return
	}
	// As with the rain, the emission cleared this spark's events.
	t.sparks.ReclaimOnEvent(ch, SceneComplete, SceneCaller("glow"), true, true)
	e.ActivateScene(ch, "glow")
	e.ActivatePath(ch, path.ID)
}

// buildStrikeCharacters makes more characters for strikes to be drawn with.
// They are added characters, so they are not part of the text and no query for
// the input finds them.
func (t *Thunderstorm) buildStrikeCharacters(e *Engine, count int) {
	for i := 0; i < count; i++ {
		t.availableStrikeChars = append(t.availableStrikeChars, e.Terminal.AddCharacter("|", C(1, 1)))
	}
}

// nextStrikeChar takes one off the free list, making more if it has run dry,
// and wipes what the last strike left on it.
func (t *Thunderstorm) nextStrikeChar(e *Engine) *Character {
	if len(t.availableStrikeChars) == 0 {
		t.buildStrikeCharacters(e, thunderstormStrikeBatch)
	}
	last := len(t.availableStrikeChars) - 1
	ch := t.availableStrikeChars[last]
	t.availableStrikeChars = t.availableStrikeChars[:last]
	ch.Animation.ClearScenes()
	ch.ClearEvents()
	return ch
}

// setupLightningStrike lays out the cells of a strike, from the top of the
// canvas down to the floor, one cell a row. Each cell leans left, leans right
// or goes straight down, and the next cell starts from where the lean left it,
// which is what gives a strike its zigzag.
//
// A cell may fork, and a fork is laid out by calling this again from the cell
// it came off. A fork does not fork again: upstream draws the random number
// either way but only takes the branch when there is no neighbour, and the
// chance drops by a hundredth each time a fork is taken.
//
// The two arm branches below are unreachable. A strike character is always
// created carrying "|", so a branch neighbour's input symbol is always "|" and
// the third arm is the one taken. Upstream is the same shape and is
// transcribed rather than trimmed.
func (t *Thunderstorm) setupLightningStrike(e *Engine, branchNeighbor *Character) {
	canvas := e.Terminal.Canvas
	var column, row int
	if branchNeighbor != nil {
		coord := branchNeighbor.Motion.CurrentCoord
		column, row = coord.Column, coord.Row
	} else {
		column, row = e.Rng.IntBetween(1, canvas.Right), canvas.Top
	}

	for row >= canvas.Bottom {
		if len(t.availableStrikeChars) == 0 {
			t.buildStrikeCharacters(e, thunderstormStrikeBatch)
		}
		var symbol string
		switch {
		case branchNeighbor == nil:
			symbol = *Choice(e.Rng, thunderstormStrikeSymbols)
		case branchNeighbor.InputSymbol == "/":
			column++
			symbol = *Choice(e.Rng, thunderstormRightArms)
		case branchNeighbor.InputSymbol == "\\":
			column--
			symbol = *Choice(e.Rng, thunderstormLeftArms)
		default:
			delta := *Choice(e.Rng, thunderstormStep)
			column += delta
			symbol = "/"
			if delta == 1 {
				symbol = "\\"
			}
		}

		strike := t.nextStrikeChar(e)
		strike.Motion.SetCoordinate(C(column, row))
		strike.Animation.SetAppearance(symbol, Fg(t.config.LightningColor), strike.UsesInputColors)
		row--
		switch symbol {
		case "\\":
			column++
		case "/":
			column--
		}

		t.pendingStrikeChars = append(t.pendingStrikeChars, strike)
		// The draw happens whether or not it can be used: it is the left side
		// of upstream's `and`.
		if e.Rng.Float() < t.strikeBranchChance && branchNeighbor == nil {
			t.strikeBranchChance -= thunderstormBranchDecay
			t.setupLightningStrike(e, strike)
		}
		branchNeighbor = nil
	}
	t.strikeBranchChance = thunderstormBranchChance
}

// lightningStrike lays out a strike and builds the scenes it and the text will
// play when it lands: a flash up to a brighter version of the colour each is
// already wearing, and for the strike itself a fade out to the terminal
// background afterwards.
//
// Errors are dropped rather than reported. Every gradient here is two stops
// and a positive step count and every frame lasts at least a tick, so none of
// these calls can fail, and Advance has nowhere to report a failure to.
func (t *Thunderstorm) lightningStrike(e *Engine) {
	t.setupLightningStrike(e, nil)

	base := t.config.LightningColor
	strikeGradient, err := NewGradientSteps(
		[]Color{base, AdjustColorBrightness(base, 1.7)}, thunderstormGradientSteps, true)
	if err != nil {
		return
	}
	fadeGradient, err := NewGradientSteps(
		[]Color{base, thunderstormBackground}, thunderstormFadeSteps, false)
	if err != nil {
		return
	}
	// The flash curve is drawn fresh for each strike, so no two strikes flash
	// with quite the same shape.
	y2 := e.Rng.Uniform(-0.6, 0.4)
	schedule := thunderstormEaseSchedule(len(strikeGradient.Spectrum), thunderstormFlashFrameDuration,
		func(progress float64) float64 { return thunderstormBezierEase(0, 1.6, 1, y2, progress) })

	for _, strike := range t.pendingStrikeChars {
		symbol := strike.Animation.CurrentVisual().Symbol

		flashColors := make([]ColorPair, len(strikeGradient.Spectrum))
		for i, color := range strikeGradient.Spectrum {
			flashColors[i] = Fg(color)
		}
		flash := strike.Animation.NewScene("flash", SceneOptions{
			UsesInputColors: strike.UsesInputColors,
			Frames:          len(schedule),
		})
		if err := thunderstormAddEasedFrames(flash, symbol, flashColors, schedule); err != nil {
			return
		}

		fade := strike.Animation.NewScene("fade", SceneOptions{
			UsesInputColors: strike.UsesInputColors,
			Frames:          len(fadeGradient.Spectrum),
		})
		for _, color := range fadeGradient.Spectrum {
			if err := fade.AddFrame(symbol, thunderstormStrikeFadeDuration,
				VisualParams{Colors: Fg(color)}); err != nil {
				return
			}
		}
		strike.Layer = 1

		strike.RegisterEvent(SceneComplete, SceneCaller("flash"), ActivateScene("fade"))
		strike.RegisterEvent(SceneComplete, SceneCaller("fade"), Callback(t.hideCharacter))
		strike.RegisterEvent(SceneComplete, SceneCaller("fade"), Callback(t.makeCharGlow))
		strike.RegisterEvent(SceneComplete, SceneCaller("fade"), Callback(t.returnStrikeToPool))
	}

	t.applyFlashEase(schedule)
}

// applyFlashEase gives every input character's flash the curve this strike
// will flash on.
//
// Upstream reaches into the scene and swaps its easing function over. This
// engine's Easing is an enumeration with no parameterised curve in it, so the
// scene is built again instead with the curve baked into its frames, which
// plays identically; see thunderstormEaseSchedule. The colours are the ones
// worked out in Build and the schedule is worked out once for the strike, so
// nothing here recomputes a gradient or walks the curve again.
func (t *Thunderstorm) applyFlashEase(schedule []thunderstormEasedRun) {
	for _, text := range t.text {
		scene := text.ch.Animation.NewScene("flash", SceneOptions{
			UsesInputColors: text.ch.UsesInputColors,
			Frames:          len(schedule),
		})
		if err := thunderstormAddEasedFrames(scene, text.ch.InputSymbol, text.flash, schedule); err != nil {
			return
		}
	}
}

// stepLightningStrike shows the next cell or two of the strike being drawn.
// When the last one appears, sparks fly out of it and everything on screen,
// strike and text alike, flashes.
func (t *Thunderstorm) stepLightningStrike(e *Engine) {
	if t.strikeProgressionDelay != 0 {
		t.strikeProgressionDelay--
		return
	}
	if len(t.pendingStrikeChars) == 0 {
		return
	}
	batch := e.Rng.IntBetween(1, 3)
	for i := 0; i < batch; i++ {
		if len(t.pendingStrikeChars) == 0 {
			break
		}
		next := t.pendingStrikeChars[0]
		t.pendingStrikeChars = t.pendingStrikeChars[1:]
		t.activeStrikeChars = append(t.activeStrikeChars, next)
		e.Terminal.SetCharacterVisibility(next, true)
		t.strikeProgressionDelay = 1

		if len(t.pendingStrikeChars) != 0 {
			continue
		}
		// The strike has landed. Sparks come off the cell that landed, and
		// the last cell is the one that reports the strike over.
		impact := t.activeStrikeChars[len(t.activeStrikeChars)-1].Motion.CurrentCoord
		for count := e.Rng.IntBetween(12, 18); count > 0; count-- {
			t.sparks.Emit(e, impact, "", true, ParticleReset{ClearEvents: true}, t.setupSparksForImpact)
		}
		next.RegisterEvent(SceneComplete, SceneCaller("fade"), Callback(t.strikeDone))

		for _, strike := range t.activeStrikeChars {
			e.ActivateScene(strike, "flash")
			e.Activate(strike)
		}
		t.activeStrikeChars = t.activeStrikeChars[:0]
		for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
			e.ActivateScene(ch, "flash")
			e.Activate(ch)
		}
	}
}

// rainFall drops a handful of raindrops in off the top of the canvas, then
// waits a few frames before the next handful.
//
// A drop can be spawned well to the left of the canvas, because it falls
// diagonally: one that starts off the left edge is what fills the bottom right
// of the screen once it has crossed.
func (t *Thunderstorm) rainFall(e *Engine) {
	if t.delay != 0 {
		t.delay--
		return
	}
	canvas := e.Terminal.Canvas
	for count := e.Rng.IntBetween(1, 6); count > 0; count-- {
		column := e.Rng.IntBetween(1-canvas.Top, canvas.Right)
		t.rain.Emit(e, C(column-1, canvas.Top+1), "", true, ParticleReset{ClearEvents: true}, t.setupRaindrop)
	}
	t.delay = e.Rng.IntBetween(1, 7)
}

// activateOnEveryCharacter starts one scene on every input character and puts
// them all back in the active set.
func (t *Thunderstorm) activateOnEveryCharacter(e *Engine, scene string) {
	for _, ch := range e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight) {
		e.ActivateScene(ch, scene)
		e.Activate(ch)
	}
}

// fadeComplete runs when the text has finished dimming. The storm starts here,
// and its clock starts with it.
func (t *Thunderstorm) fadeComplete(e *Engine, ch *Character) {
	t.phase = thunderstormStorm
	t.stormStartTime = e.Clock.Elapsed()
}

// hideCharacter takes a spent strike cell off the screen.
func (t *Thunderstorm) hideCharacter(e *Engine, ch *Character) {
	e.Terminal.SetCharacterVisibility(ch, false)
}

// makeCharGlow lights up the input character the strike cell was standing on,
// if there is one and it is on screen.
func (t *Thunderstorm) makeCharGlow(e *Engine, ch *Character) {
	under := e.Terminal.CharacterAtInputCoord(ch.Motion.CurrentCoord)
	if under == nil || !under.IsVisible {
		return
	}
	e.ActivateScene(under, "glow")
	t.pendingGlowChars = append(t.pendingGlowChars, under)
}

// returnStrikeToPool puts a spent strike cell back on the free list.
func (t *Thunderstorm) returnStrikeToPool(e *Engine, ch *Character) {
	t.availableStrikeChars = append(t.availableStrikeChars, ch)
}

// strikeDone reports the strike over, which lets the next one start and lets
// the storm end.
func (t *Thunderstorm) strikeDone(e *Engine, ch *Character) {
	t.strikeInProgress = false
}

// Build stocks the two particle pools and the strike characters, then gives
// every input character the four scenes it needs: the dim it starts with, the
// brighten it ends with, the flash a strike sets off and the glow a strike
// leaves behind.
func (t *Thunderstorm) Build(e *Engine) error {
	var err error
	t.rain, err = NewParticlePool(t.config.RaindropSymbols, 0, Coord{}, thunderstormInitRaindrop)
	if err != nil {
		return err
	}
	if err := t.rain.Preallocate(e, thunderstormRaindropCount); err != nil {
		return err
	}
	t.sparkGradient, err = NewGradientSteps(
		[]Color{t.config.SparkGlowColor, thunderstormBackground}, thunderstormGradientSteps, false)
	if err != nil {
		return err
	}
	t.sparks, err = NewParticlePool(t.config.SparkSymbols, 2000, Coord{}, t.initializeSpark)
	if err != nil {
		return err
	}
	if err := t.sparks.Preallocate(e, thunderstormSparkCount); err != nil {
		return err
	}
	t.stormStartTime = e.Clock.Elapsed()

	finalGradient, err := NewGradient(t.config.FinalGradientStops, t.config.FinalGradientSteps, false)
	if err != nil {
		return err
	}
	canvas := e.Terminal.Canvas
	mapping, err := finalGradient.BuildCoordinateColorMapping(
		canvas.TextBottom, canvas.TextTop, canvas.TextLeft, canvas.TextRight,
		t.config.FinalGradientDirection)
	if err != nil {
		return err
	}
	fallback := finalGradient.Spectrum[0]

	t.buildStrikeCharacters(e, thunderstormStrikePool)

	dynamic := e.Terminal.Config.ExistingColorHandling == DynamicExistingColors
	characters := e.Terminal.GetCharacters(e.Rng, InputOnly(), SortTopToBottomLeftToRight)
	t.text = make([]thunderstormText, 0, len(characters))
	for _, ch := range characters {
		input := ch.Animation.InputColors

		// visible is what the character wears with the sky clear, and restore
		// is what it must be left wearing at the end. They differ only for a
		// character that arrived with no foreground of its own: it dims away
		// from a neutral grey, because there is nothing else to dim, but it
		// has to end up with no foreground again rather than wearing the grey.
		var visible, restore ColorPair
		if dynamic {
			visible = ColorPair{Fg: thunderstormNeutralGray, HasFg: true, Bg: input.Bg, HasBg: input.HasBg}
			if input.HasFg {
				visible.Fg = input.Fg
			}
			restore = input
		} else {
			visible = Fg(mapping.At(ch.InputCoord, fallback))
			restore = visible
		}
		storm := thunderstormAdjustPair(visible, 0.5)

		if err := t.addGlowScene(ch, storm, dynamic); err != nil {
			return err
		}
		if err := t.addFadeScene(ch, visible, storm, dynamic); err != nil {
			return err
		}
		if err := t.addUnfadeScene(ch, visible, storm, restore, dynamic); err != nil {
			return err
		}
		flash, err := t.addFlashScene(ch, visible, storm)
		if err != nil {
			return err
		}
		t.text = append(t.text, thunderstormText{ch: ch, flash: flash})

		// The picture is already on the screen, so every character is shown
		// from the first frame rather than revealed as the effect reaches it.
		e.Terminal.SetCharacterVisibility(ch, true)
	}

	if len(characters) == 0 {
		// Deviation, and the only one that is not scoped to a colour policy:
		// a screen with nothing on it has no storm to run. Upstream takes the
		// first character here and would fail on an empty screen. Ending
		// straight away is the alternative to sitting in the waiting phase
		// for ever, with no character left to report the dimming finished.
		t.phase = thunderstormComplete
		return nil
	}
	// One character speaks for all of them: they all start dimming on the
	// same frame and dim at the same rate, so the first one to finish is the
	// signal that the sky is dark enough for the storm.
	characters[0].RegisterEvent(SceneComplete, SceneCaller("fade"), Callback(t.fadeComplete))
	return nil
}

// addGlowScene builds the cooling a character runs through after a strike has
// passed over it, from the glow colour back down to its stormy self.
func (t *Thunderstorm) addGlowScene(ch *Character, storm ColorPair, dynamic bool) error {
	gradient, err := NewGradientSteps(
		[]Color{t.config.GlowingTextColor, storm.Fg}, thunderstormGradientSteps, false)
	if err != nil {
		return err
	}
	scene := ch.Animation.NewScene("glow", SceneOptions{
		UsesInputColors: ch.UsesInputColors,
		Frames:          len(gradient.Spectrum) + 1,
	})
	for _, color := range gradient.Spectrum {
		colors := ColorPair{Fg: color, HasFg: true, Bg: storm.Bg, HasBg: storm.HasBg}
		if err := scene.AddFrame(ch.InputSymbol, t.config.TextGlowTime, VisualParams{Colors: colors}); err != nil {
			return err
		}
	}
	if dynamic {
		// The ramp above stops one colour short of the storm colour, so the
		// character is put back on it explicitly. Without this a cell that
		// carried a background would be left a shade off the one everything
		// around it is wearing.
		return scene.AddFrame(ch.InputSymbol, t.config.TextGlowTime, VisualParams{Colors: storm})
	}
	return nil
}

// addFadeScene builds the dimming the text runs through before the storm.
func (t *Thunderstorm) addFadeScene(ch *Character, visible, storm ColorPair, dynamic bool) error {
	scene := ch.Animation.NewScene("fade", SceneOptions{
		UsesInputColors: ch.UsesInputColors,
		Frames:          thunderstormGradientSteps + 2,
	})
	if dynamic {
		if err := thunderstormPairGradientFrames(scene, ch.InputSymbol, visible, storm,
			thunderstormGradientSteps, thunderstormTextFadeDuration); err != nil {
			return err
		}
		return scene.AddFrame(ch.InputSymbol, thunderstormTextFadeDuration, VisualParams{Colors: storm})
	}
	gradient, err := NewGradientSteps([]Color{visible.Fg, storm.Fg}, thunderstormGradientSteps, false)
	if err != nil {
		return err
	}
	for _, color := range gradient.Spectrum {
		if err := scene.AddFrame(ch.InputSymbol, thunderstormTextFadeDuration,
			VisualParams{Colors: Fg(color)}); err != nil {
			return err
		}
	}
	return nil
}

// addUnfadeScene builds the brightening the text runs through once the sky has
// cleared. It is the dimming backwards, and under the dynamic policy it ends
// on the colours the character arrived with rather than on the ones it was
// shown in, which is the same thing for every character that had a foreground
// of its own.
func (t *Thunderstorm) addUnfadeScene(ch *Character, visible, storm, restore ColorPair, dynamic bool) error {
	scene := ch.Animation.NewScene("unfade", SceneOptions{
		UsesInputColors: ch.UsesInputColors,
		Frames:          thunderstormGradientSteps + 3,
	})
	if dynamic {
		if err := thunderstormPairGradientFrames(scene, ch.InputSymbol, storm, visible,
			thunderstormGradientSteps, thunderstormTextFadeDuration); err != nil {
			return err
		}
		if err := scene.AddFrame(ch.InputSymbol, thunderstormTextFadeDuration,
			VisualParams{Colors: visible}); err != nil {
			return err
		}
		if restore != visible {
			return scene.AddFrame(ch.InputSymbol, thunderstormTextFadeDuration,
				VisualParams{Colors: restore})
		}
		return nil
	}
	gradient, err := NewGradientSteps([]Color{visible.Fg, storm.Fg}, thunderstormGradientSteps, false)
	if err != nil {
		return err
	}
	for i := len(gradient.Spectrum) - 1; i >= 0; i-- {
		if err := scene.AddFrame(ch.InputSymbol, thunderstormTextFadeDuration,
			VisualParams{Colors: Fg(gradient.Spectrum[i])}); err != nil {
			return err
		}
	}
	return nil
}

// addFlashScene builds the flash a strike sets off across the whole screen: a
// loop from the stormy colour up to a brighter version of the clear-sky one
// and back down. It also returns the colours, because each strike rebuilds
// this scene with its own curve baked in; see applyFlashEase.
func (t *Thunderstorm) addFlashScene(ch *Character, visible, storm ColorPair) ([]ColorPair, error) {
	gradient, err := NewGradientSteps(
		[]Color{storm.Fg, AdjustColorBrightness(visible.Fg, 1.7)}, thunderstormGradientSteps, true)
	if err != nil {
		return nil, err
	}
	colors := make([]ColorPair, len(gradient.Spectrum))
	for i, color := range gradient.Spectrum {
		colors[i] = ColorPair{Fg: color, HasFg: true, Bg: storm.Bg, HasBg: storm.HasBg}
	}
	scene := ch.Animation.NewScene("flash", SceneOptions{
		UsesInputColors: ch.UsesInputColors,
		Frames:          len(colors),
	})
	for _, pair := range colors {
		if err := scene.AddFrame(ch.InputSymbol, thunderstormFlashFrameDuration,
			VisualParams{Colors: pair}); err != nil {
			return nil, err
		}
	}
	return colors, nil
}

// Advance runs one frame.
func (t *Thunderstorm) Advance(e *Engine) bool {
	if e.ActiveCount() == 0 && t.phase == thunderstormComplete {
		return false
	}
	switch t.phase {
	case thunderstormPreStorm:
		t.activateOnEveryCharacter(e, "fade")
		t.phase = thunderstormWaiting
	case thunderstormStorm:
		t.rainFall(e)
		if !t.strikeInProgress && e.Rng.Float() < thunderstormStrikeChance {
			t.strikeInProgress = true
			t.lightningStrike(e)
		}
		if t.strikeInProgress {
			t.stepLightningStrike(e)
		}
		for _, ch := range t.pendingGlowChars {
			e.Activate(ch)
		}
		t.pendingGlowChars = t.pendingGlowChars[:0]
		if e.Clock.Elapsed()-t.stormStartTime >= float64(t.config.StormTime) && !t.strikeInProgress {
			t.activateOnEveryCharacter(e, "unfade")
			t.phase = thunderstormComplete
		}
	}
	e.Update()
	// The rain, the sparks and the cells of a strike all carry a foreground
	// and no background and all sit on the layer above the text, so without
	// this they take the fill out of whatever they are crossing, and the rain
	// crosses the whole screen.
	carryAddedCharactersOverBackgrounds(e)
	return true
}
