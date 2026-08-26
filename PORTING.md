# Porting an effect

This is how to bring one more effect across from ttfx. It is written for
someone doing exactly one, in one file, without needing to change the engine.

If you find yourself wanting to change the engine, stop and say so instead.
That is a signal the engine is missing something, and thirty people each
working around the same gap in their own file is the outcome this document
exists to prevent.

## Where the source is

    TerminalTextEffects   https://github.com/ChrisBuilds/terminaltexteffects
                          terminaltexteffects/effects/effect_<name>.py
    ttfx                  https://github.com/omacom-io/ttfx
                          src/effects/<name>.rs

Port from the **Rust**. It has already resolved the parts of the Python that
are ambiguous, and its comments record which upstream quirks are deliberate.
Read the Python only when a Rust comment points you at it.

## The shape of a port

One file, `effect_<name>.go`. Four things in it:

```go
func init() {
    Register(Descriptor{
        Name:        "<name>",
        Description: "One line, plain English, no marketing.",
        New:         func() Effect { return New<Name>(Default<Name>Config()) },
    })
}

type <Name>Config struct { /* the ttfx clap Args struct, as plain fields */ }

func Default<Name>Config() <Name>Config { /* the clap default_value entries */ }

type <Name> struct { /* the ttfx struct, minus the config */ }

func New<Name>(config <Name>Config) *<Name> { ... }

func (x *<Name>) Build(e *Engine) error   { /* ttfx build()      */ }
func (x *<Name>) Advance(e *Engine) bool  { /* ttfx next_frame() */ }
```

`Advance` returns whether the effect is still running. It does **not** return
the frame: the host reads that with `engine.Frame()` or `engine.FrameRows()`.
Where ttfx writes `return Some(ctx.frame())`, you write `return true`; where it
writes `return None`, you write `return false`.

## Rust to Go, call for call

| ttfx | here |
| --- | --- |
| `ctx.terminal.arena[id.0 as usize]` | the `*Character` itself; there is no arena |
| `CharId` | `*Character` |
| `ctx.terminal.get_characters(&mut ctx.rng, filter, sort)` | `e.Terminal.GetCharacters(e.Rng, filter, sort)` |
| `CharacterFilter::default()` | `InputOnly()` |
| `ctx.terminal.get_characters_grouped(filter, group)` | `e.Terminal.GetCharactersGrouped(filter, group)` |
| `ctx.terminal.set_character_visibility(id, true)` | `e.Terminal.SetCharacterVisibility(ch, true)` |
| `ch.animation.new_scene(loop, sync, ease, id, uses_pre)` | `ch.Animation.NewScene(id, SceneOptions{...})` |
| `scene.add_frame(sym, dur, params)` | `scene.AddFrame(sym, dur, VisualParams{...})` |
| `scene.apply_gradient_to_symbols(...)` | `scene.ApplyGradientToSymbols(...)` |
| `ch.motion.new_path(speed, ease, layer, hold, loop, id)` | `ch.Motion.NewPath(id, PathOptions{...})` |
| `path.new_waypoint(coord, bezier, id)` | `path.NewWaypoint(coord, bezier, id)` |
| `ctx.activate_scene(self, id, "x")` | `e.ActivateScene(ch, "x")` |
| `ctx.activate_path(self, id, "x")` | `e.ActivatePath(ch, "x")` |
| `ctx.register_event(id, ev, CallerKey::Scene(s), act)` | `ch.RegisterEvent(ev, SceneCaller(s), act)` |
| `EventAction::ActivateScene("x")` | `ActivateScene("x")` |
| `ctx.active_characters.insert(id)` | `e.Activate(ch)` |
| `ctx.active_characters.is_empty()` | `e.ActiveCount() == 0` |
| `ctx.update(self)` | `e.Update()` |
| `ctx.rng.randint(a, b)` | `e.Rng.IntBetween(a, b)` (both ends included) |
| `ctx.rng.randrange(a, b)` | `e.Rng.IntBelow(a, b)` (top excluded) |
| `ctx.rng.random()` | `e.Rng.Float()` |
| `ctx.rng.uniform(a, b)` | `e.Rng.Uniform(a, b)` |
| `ctx.rng.choice(&xs)` | `Choice(e.Rng, xs)` (a free function; returns a pointer) |
| `ctx.rng.shuffle(&mut xs)` | `Shuffle(e.Rng, xs)` |
| `Gradient::new(stops, steps, _, loop)` | `NewGradient(stops, steps, loop)` |
| `Gradient::with_steps(stops, n, loop)` | `NewGradientSteps(stops, n, loop)` |
| `Animation::adjust_color_brightness(c, f)` | `AdjustColorBrightness(c, f)` |
| `Color::from_hex("ab48ff").unwrap()` | `MustParseColor("ab48ff")` |
| `ColorPair::new(Some(fg), None)` | `Fg(fg)` |
| `ColorPair::new(Some(fg), Some(bg))` | `FgBg(fg, bg)` |

### Callbacks

ttfx routes `EventAction::Callback` through a numeric id and an
`EffectHooks::dispatch_callback` match, because Rust's ownership rules will not
let it hold a closure in the arena. Go has no such problem. Write the closure:

```go
ch.RegisterEvent(SceneComplete, SceneCaller("gradient"), Callback(x.onCycleDone))
```

with `func (x *Thing) onCycleDone(e *Engine, ch *Character)`. Delete the id
constants and the `dispatch_callback` implementation entirely.

### There is no EffectHooks

Every ttfx `impl EffectHooks for X` block disappears. Anywhere ttfx passes
`self` into an engine call so it can dispatch callbacks, just drop the
argument.

## Things that will bite you

**Rows count up from the bottom.** Row 1 is the bottom of the canvas, row
`canvas.Top` is the top. This matches ttfx. If your effect looks vertically
mirrored, this is why.

**A scene id of `""` is auto-allocated** and `NewScene` returns the `*Scene`,
so use `scene.ID` rather than guessing what it was called. Same for paths.

**`Frames` in `SceneOptions` is a capacity hint.** Set it when you know how
many frames you are about to add. Nothing depends on it being right; a scene
with eighty frames regrows its slices seven times without it, and over a full
screen that regrowth is most of what the build allocates.

**A looping scene reports itself complete.** `ActiveSceneIsComplete` returns
true for a looping scene, so a character that only loops reads as inactive and
`Update` drops it. That is upstream behaviour and several effects rely on it.
If your effect stops early, check this first.

**`e.ActiveCharacters()` reuses its slice.** Do not hold the result across an
`Update`.

**Declare fill characters.** If your effect queries `InnerFill` or `OuterFill`,
or builds a spanning tree, set `NeedsFillCharacters: true` in the `Descriptor`.
The terminal is built before the effect and a fill character cannot be added
later, so without the declaration you get an empty set and silently animate
nothing. Upstream always makes fill characters, so no ttfx effect says it needs
them; yours has to.

## The three rounding quirks

These are wrong on purpose. Do not fix them, and do not route around them.

1. **Half to even.** `roundHalfEven` is Python's `round()`. Coordinates land on
   it everywhere. Plain rounding shifts paths by a cell on about half the ties.
2. **Integer floor division in gradients.** Channel deltas use `floorDiv`, not
   a float lerp, and the exact end stop is appended after each pair. This is
   what gives a ramp its banding, and every effect's colours are tuned to it.
3. **The bezier arc length stops at t=0.9.** `FindLengthOfBezierCurve` walks
   nine tenths of the curve and never adds the last tenth, so every length
   comes out short. Path speed divides by this number. Correcting it makes
   every curved path in every effect finish sooner than its author tuned it to.

`ShiftColorTowards` truncates to an integer channel and `AdjustColorBrightness`
rounds half to even. That difference is also upstream's and also deliberate.

## The colour policy

`TerminalConfig.ExistingColorHandling` decides what happens to colours the
input already carried:

* `IgnoreExistingColors` (default) throws them away and uses the effect's own
  gradient. This is upstream's default and the mode to match when in doubt.
* `DynamicExistingColors` resolves every character back to the colour it
  arrived with. This is the mode a screen saver runs in: the input is a
  picture that was already on the screen and it must reassemble as itself.
* `AlwaysExistingColors` pins every frame to the input colour.

Nearly every ttfx effect has a `dynamic` branch in `build()`. Port it. It is
usually the difference between `final = ch.Animation.InputColors` and
`final = Fg(mapping.At(ch.InputCoord, fallback))`.

### Deviating for a screen

Upstream is written for piped text. Three of the ports here needed a change
because a captured screen is not piped text, and a fourth will probably need
one too. The rule is: **scope the change to `DynamicExistingColors`, so the
default behaviour stays exactly upstream's, and comment it where you make it.**

The three so far, as worked examples:

* **Backgrounds must survive.** An effect that sets only a foreground erases
  the background a captured cell carried. On a screen that is every selection
  bar, filled panel and piece of window chrome, and they blink out for the
  length of the effect. `vhstape` carries the background through its colour
  fringing for this reason.
* **A ramp starts from where the character is.** Once you carry a background,
  the closing ramp must start from that background and not from the
  foreground's last colour, or a blue bar flushes red before it settles.
* **The picture is already on screen.** Upstream hides every character and
  reveals it when the effect reaches it, which is right for text arriving from
  nothing. For a sweep like `waves` it meant the wave crossed an empty canvas
  with the picture trailing behind it. Under `DynamicExistingColors`, show
  every character from the first frame wearing the colour it will settle back
  to, and let the effect take it from there.

That last one applies to any effect that passes **over** the screen rather than
assembling it. Ask which yours is.

## Wide glyphs

The host drops glyphs wider than one cell before the engine sees them, because
the engine places one character per column and a two-cell glyph would shear the
rest of its row. You do not need to handle this, but do not write an effect
that depends on precise cell geometry matching the source text, because on a
real screen it will not.

## What a finished port must include

1. **The file**, `effect_<name>.go`, with the `init` registration.
2. **A test** in `effect_<name>_test.go` with, at minimum:
   * it runs to completion within a frame cap and the final frame equals the
     input text;
   * it actually animates, so a middle frame differs from the input;
   * the thing the effect is *named for* happens. `rain` starts at the top of
     the canvas; `vhstape` aims a whole row at one offset. This is the test
     that catches a port which resolves correctly and looks wrong.
   * **Every test states its negative control in its doc comment, and you
     have run that control and watched it fail.** A control that passes means
     the test is not testing what it says. Two of the tests here were caught
     that way and rewritten. If a control does not fail, say so in the comment
     and describe the one that does.
3. **A NOTICE entry**, one line, mapping your file to both its ttfx source and
   its TerminalTextEffects source. Keep the column alignment.
4. **A README table row** naming the effect and what it exercises.
5. **A look at it.** Render frames and look at them. Both of the real bugs
   found in the ports here passed every test and were visible in the first
   frame anybody looked at.

## Helpers that already exist

Before writing a local version of something, check for it. All of these are
ported and tested:

`FindLengthOfLine`, `FindCoordOnLine`, `FindCoordOnBezierCurve`,
`FindLengthOfBezierCurve`, `FindCoordsOnCircle`, `FindCoordsInCircle`,
`FindCoordsInRect`, `FindCoordsOnRect`, `ExtrapolateAlongRay`,
`FindNormalizedDistanceFromCenter`, `AdjustColorBrightness`,
`ShiftColorTowards`, `NewGradient`, `BuildCoordinateColorMapping`, the
thirty-one easing curves, and the character sorts and groupings.

Three more arrived after the first four effects, and each has its own section
below because each deviates from the Rust in a way you have to know about:
the **spanning tree generators**, the **particle pool**, and the **clock**.

## Spanning trees

`spanning_tree.go`, from ttfx `src/utils/spanning_tree.rs`, from TTE
`utils/spanningtree/`.

Four generators join the characters of the canvas into a tree, one edge per
`Step`, and record the order they did it in. That order is the effect's running
order.

| Generator | What its tree looks like | Who uses it |
| --- | --- | --- |
| `PrimsSimple` | wanders outwards from a point | `burn` |
| `PrimsWeighted` | seeps towards the cheapest cell first | `smoke` |
| `RecursiveBacktracker` | long corridors with dead ends | `laseretch` |
| `BreadthFirst` | walks a tree somebody else built, one ring per step | `smoke` |

All four share a shape:

```go
tree, err := NewPrimsSimple(e, nil, true)   // nil starting character = random
for !tree.Complete {
    tree.Step(e)
}
for _, ch := range tree.CharLinkOrder { ... }
```

In practice you call `Step` once per frame instead, so the tree grows on
screen while the effect runs.

### Call for call

| ttfx | here |
| --- | --- |
| `PrimsSimple::new(ctx, start, limit)` | `NewPrimsSimple(e, start, limit)` |
| `PrimsWeighted::new(ctx, start, limit)` | `NewPrimsWeighted(e, start, limit)` |
| `RecursiveBacktracker::new(ctx, start, limit)` | `NewRecursiveBacktracker(e, start, limit)` |
| `BreadthFirst::new(ctx, start, limit)` | `NewBreadthFirst(e, start, limit)` |
| `Some(char_id)` / `None` for the start | a `*Character`, or `nil` for a random one |
| `algo.step(ctx)` | `algo.Step(e)` |
| `bfs.step(ctx)` | `bfs.Step()`, which needs no engine because it only follows links |
| `link_characters(ctx, a, b)` | `LinkCharacters(a, b)`, no engine either |
| `ch.links` | `ch.Links`, ascending id, same as ttfx |
| `ch.neighbors.north` and friends | `e.Terminal.Neighbors(ch)`, in north, east, south, west order, skipping empty cells |
| `algo.char_link_order` | `algo.CharLinkOrder` |
| `algo.char_last_linked` | `algo.CharLastLinked` |
| `algo.complete` | `algo.Complete` |
| `bfs.explored_last_step` | `bfs.ExploredLastStep` |
| `bfs.char_explore_order` | `bfs.CharExploreOrder` |
| the rest of the public fields | the same name, exported |

### Where this differs from the Rust

**`LinkCharacters` takes no engine.** ttfx passes `ctx` because its characters
live in an arena and it needs the arena to reach them. Here a character is a
pointer. Same for `BreadthFirst.Step`, which reads only `Links`.

**Neighbours are looked up, not stored.** ttfx snapshots four slots onto every
character when the terminal is built; `Terminal.Neighbors` reads the same
input-coordinate table on demand. The two agree, because that table never
changes after the terminal is built: `AddCharacter` deliberately stays out of
it, so a character you created yourself has no neighbours in either
implementation.

### The two things that will catch you

**Your effect must set `NeedsFillCharacters: true`.** The tree runs over the
whole canvas, not over the text. Without fill characters most cells hold no
character, and a generator handed `nil` for its starting character picks a
random coordinate that lands on nothing and returns `ErrNoStartingCharacter`.
Upstream always makes fill characters, so it never had to say this.

**`Complete` turns true one `Step` late.** It flips on the step that finds the
work already finished, not on the step that finishes it, so there is always one
last step that links nothing. That is upstream's, `BreadthFirst` does it too,
and effect frame counts are tuned to it. Do not tighten it.

## The particle pool

`particles.go`, from ttfx `src/engine/particles.rs`, from TTE
`engine/effect_support/particles.py`.

`ParticlePool` recycles the characters an effect throws off: sparks, smoke,
raindrops. A character lives on the terminal for the whole run, so an effect
that made a new one per spark would leak for as long as it ran.

```go
pool, err := NewParticlePool([]string{"*", "."}, 2000, Coord{}, initSpark)
pool.Preallocate(e, 2000)
for _, particle := range pool.Particles {
    pool.ReclaimOnEvent(particle, SceneComplete, SceneCaller("spark"), true, true)
}
...
pool.Emit(e, origin, "", true, ParticleReset{}, func(e *Engine, ch *Character) {
    // give it this flight's path and scene, then activate them
})
```

### Call for call

| ttfx | here |
| --- | --- |
| `ParticlePool::new(symbols, max, coord)` | `NewParticlePool(symbols, max, coord, initializer)` |
| `Some(2000)` / `None` for the max | `2000` / `0` |
| `None` for the coord | `Coord{}` |
| `pool.preallocate(ctx, n, initializer)` | `pool.Preallocate(e, n)` |
| `pool.acquire(ctx, symbol, reset, initializer)` | `pool.Acquire(e, symbol, reset)` |
| `pool.emit(ctx, origin, symbol, visible, reset, initializer, on_emit)` | `pool.Emit(e, origin, symbol, visible, reset, onEmit)` |
| `Some("x")` / `None` for a symbol | `"x"` / `""` |
| `pool.reclaim(ctx, id, hide, deactivate)` | `pool.Reclaim(e, ch, hide, deactivate)` |
| the hand-rolled reclaim callback registration | `pool.ReclaimOnEvent(ch, event, from, hide, deactivate)` |
| `pool.extend(ids)` | `pool.Extend(chars...)` |
| `pool.particles` | `pool.Particles` |
| `pool.len()` | `pool.Len()` |
| `ParticleReset::default()` | `ParticleReset{}` |
| `ParticleReset { clear_events: true, ..Default::default() }` | `ParticleReset{ClearEvents: true}` |

`Emit` and `Acquire` return `nil` when the pool is capped out and nothing is
free. An effect throwing off decoration can ignore that; check it if the
particle matters.

### Where this differs from the Rust

**The pool holds the initializer.** ttfx passes it to `preallocate`, `acquire`
and `emit` separately, because Rust will not let the pool own a closure that
touches the engine. Every effect in the catalogue passes the same closure to
all three, and passing two different ones would give you particles that behave
differently depending on whether the free list happened to be empty. So it is
given once, to `NewParticlePool`, and it may be `nil`.

It runs once per particle **created**, never on one reused. It is where you
build the scenes a particle needs for its whole life. Anything that changes per
flight belongs in `onEmit`.

**Three `ParticleReset` fields are inverted, on purpose.** Upstream names them
`clear_paths`, `deactivate_path` and `deactivate_scene` and defaults all three
to **true**. A Go struct defaults to false, so keeping upstream's names would
make `ParticleReset{}` mean "reset nothing" and hand you a spark still flying
along its last path. Here they are `KeepPaths`, `KeepActivePath` and
`KeepActiveScene`, so:

    ParticleReset{}                     == upstream's default
    ParticleReset{ClearEvents: true}    == the only other shape any effect uses

If you find yourself wanting upstream's `clear_paths: false`, that is
`KeepPaths: true`. Reach for `ClearScenes` only if you know why: it throws away
the scenes the initializer built and makes you rebuild them every emission,
which is the cost the pool exists to avoid.

**`ReclaimOnEvent` exists here.** Upstream has it; ttfx dropped it and each
effect wires the registration by hand through a callback id, because of the
same Rust closure problem. Use it.

**No `emission_id`.** ttfx's `burn` threads a counter through its reclaim
callback so each registration is a distinct closure, because Python raises on
registering the identical handler twice. Nothing here rejects a duplicate
registration and `Reclaim` is idempotent, so the counter has no job. Delete it.

## The clock

`clock.go`, from ttfx `src/engine/ctx.rs` `Clock`.

Two effects are written in seconds rather than frames: `matrix` runs its rain
for a number of seconds, `thunderstorm` its storm.

| ttfx | here |
| --- | --- |
| `ctx.clock.now_wall()` | `e.Clock.Wall()` |
| `ctx.clock.now_monotonic()` | `e.Clock.Elapsed()` |
| `ctx.clock.advance_frame()` | nothing; `e.Update()` does it |

Both return `float64` seconds, as they do in ttfx.

### Keeping your port deterministic

**`NewEngine` installs a virtual clock, and you should leave it there.**
Everything else in this engine comes out of a seed, and a real clock would take
that away from exactly the two effects that read it: the same seed would give a
different number of frames on a fast machine than on a slow one, and neither
effect could be tested.

The virtual clock advances one step per `Engine.Update`, and an effect calls
`Update` exactly once per frame. So it reports the time the animation *would*
have taken, `rain_time` still means seconds, and a seeded run is repeatable.
Write your effect against it exactly as ttfx writes its effect against
`now_wall`, and you get both.

Three rules follow:

* **Do not call `AdvanceFrame` yourself.** `Update` calls it. Calling it too
  runs your effect's clock at twice the frame rate.
* **Do not read the clock to decide how far to move something.** It is there
  for "has long enough passed", which is all upstream uses it for. Anything
  smoother should count frames, like every other effect here does.
* **Do not swap in `NewRealClock`.** It exists for a host that wants wall time
  and accepts losing the repeatable run. It is not an effect's decision.

The host sets the frame rate: `e.Clock = NewVirtualClock(30)` if it paints at
thirty. Your effect never touches this.
