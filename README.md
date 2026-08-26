# tuiffects

Terminal text effects as a Go library. Feed it text or a captured cell grid,
pick an effect, and pull frames off it one at a time.

## Credit where it is due

**This is a port of a port, and it originates none of the art.**

[TerminalTextEffects][tte] by [ChrisBuilds][chrisbuilds] is the original.
Every effect in this package, and the architecture of the engine that runs
them, are that project's design. [ttfx][ttfx] by omacom-io translated that work
to Rust and says the same thing about itself. This package translates ttfx to
Go.

All three are MIT. All three copyrights are preserved in [LICENSE](LICENSE),
and [NOTICE](NOTICE) maps every file here to both the ttfx source and the
TerminalTextEffects source it came from.

If you like what you see, star [the original][tte]. Effect ideas belong
upstream, where they were invented.

[tte]: https://github.com/ChrisBuilds/terminaltexteffects
[chrisbuilds]: https://github.com/ChrisBuilds
[ttfx]: https://github.com/omacom-io/ttfx

## Using it

```go
import "github.com/Gaurav-Gosain/tuiffects"
```

```go
terminal := tuiffects.NewTerminalFromText("hello", tuiffects.TerminalConfig{
    Width: 80, Height: 24,
})
engine := tuiffects.NewEngine(terminal, tuiffects.NewRng(1))

effect := tuiffects.NewDecrypt(tuiffects.DefaultDecryptConfig())
if err := effect.Build(engine); err != nil {
    return err
}
for effect.Advance(engine) {
    fmt.Print("\x1b[H", engine.Frame())
}
```

`Advance` does not return the frame. Read it with `engine.Frame()` for an ANSI
string, or `engine.FrameRows()` for rows of visuals you can style yourself.
That is the one place this port deliberately differs in shape from ttfx, which
writes to a tty it owns. It is what lets this drive a pane, a widget or a
screen saver rather than a terminal.

To animate a screen rather than a string, hand it a cell grid:

```go
terminal := tuiffects.NewTerminalFromCells(cells, tuiffects.TerminalConfig{
    Width:                 cols,
    Height:                rows,
    ExistingColorHandling: tuiffects.DynamicExistingColors,
})
```

`DynamicExistingColors` makes every character resolve back to the colour it
arrived with, so the screen reassembles as itself rather than in the effect's
own palette. It is the mode to use when the input is a picture that was
already on the screen.

## What is here

The engine, in the shape ttfx found it:

| Piece | What it is |
| --- | --- |
| `Coord`, `geometry.go` | 1-based grid coordinates, origin bottom left, lines and bezier curves |
| `Color`, `Gradient` | colour ramps and the coordinate mappings effects paint across the canvas |
| `Easing` | the thirty-one standard curves |
| `Waypoint`, `Path`, `Motion` | where a character goes and how fast |
| `Frame`, `Scene`, `Animation` | what a character looks like over time |
| `Event`, `Action` | how a scene or path hands off to the next one |
| `Character` | one cell: its animation, its motion, its handlers |
| `Canvas`, `Terminal` | the grid, the character populations, and the frame painter |
| `ParticlePool` | recycles the short-lived characters an effect throws off |
| `PrimsSimple` and the other spanning trees | join the canvas into a tree, and give an effect its running order |
| `Clock` | seconds, for the effects written in seconds rather than frames |
| `Engine` | the stepping loop that ties all of it together |

Four effects, chosen to exercise different parts of that engine and to work
over a whole screen of arbitrary content rather than over a centred banner:

| Effect | What it does | What it shows |
| --- | --- | --- |
| `decrypt` | types out ciphertext, then decrypts it | per-character scenes and scene-to-scene chaining, no motion at all |
| `rain` | characters fall in and settle | paths, easing, and a path completion handing off to an animation |
| `waves` | a band of blocks sweeps across | eased scenes released in bands, a sweep with no motion at all |
| `vhstape` | rows slip and the picture is redrawn | paths driving synced scenes, row groups, and several phases |

## Adding an effect

Write one file. Implement `Build` (set up scenes and paths on every character)
and `Advance` (release a few characters, call `engine.Update()`, say whether
you are done), and call `Register` from an `init`. The four here are 160 to 430
lines each and the engine does the rest.

[PORTING.md](PORTING.md) is the full guide for bringing one across from ttfx:
the call-for-call mapping, the quirks that are wrong on purpose, what the
colour policy does, and what a finished port has to include.

## Differences from ttfx

* No parity with the Python original, and no Mersenne Twister clone. The same
  effect will not produce the same frames as either upstream. `NewRng(seed)`
  makes a run reproducible within this package, which is what the tests need.
* Time is virtual by default. The engine's clock advances one frame's worth per
  `Update` rather than reading the machine, so an effect written in seconds
  runs to the same number of frames every time. `NewRealClock` is there for a
  host that would rather have wall time.
* No command line, no tty writer, no resize handling. The host owns the screen.
* Four effects rather than thirty-five.
* Rounding quirks that change how effects look **are** kept: half-to-even
  rounding on coordinates, floor division on gradient channel steps, and the
  bezier arc-length estimate that stops at t=0.9. Removing them would retune
  every effect by a little, silently.
* A few effects behave differently under `DynamicExistingColors`, because
  upstream is written for piped text and that mode means the input was already
  on the screen. Each is commented where it is made, and the default behaviour
  is unchanged.

## Licence

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
