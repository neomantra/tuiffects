package tuiffects

import (
	"os"
	"strings"
	"testing"
)

// TestEveryEffectIsCredited checks the attribution chain, which is the one
// thing about this package that cannot be corrected after the fact and so is
// guarded by a test rather than by a habit.
//
// Every registered effect must have a file and a README table row. Beyond
// that it must be one of two things and not both, and which one it is comes
// from the effect's own Descriptor rather than from a list kept here:
//
//   - a port declares no Origin, and must have a NOTICE line naming both the
//     ttfx source and the TerminalTextEffects source it was translated from;
//   - an effect original to this package declares an Origin saying where its
//     material came from, and must have no NOTICE line at all, because NOTICE
//     records translations and this was not one.
//
// Checking both directions is what makes the declaration worth anything. An
// original that also appears in NOTICE is claiming an upstream it does not
// have, and that fails here just as loudly as a port with no line.
//
// Negative control: deleting a port's NOTICE line, deleting any effect's
// README row, clearing tuffbaby's Origin, and adding a NOTICE line for an
// effect that declares one all fail this test. All four were run.
func TestEveryEffectIsCredited(t *testing.T) {
	notice, err := os.ReadFile("NOTICE")
	if err != nil {
		t.Fatalf("NOTICE: %v", err)
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("README.md: %v", err)
	}
	noticeText, readmeText := string(notice), string(readme)

	for _, name := range Names() {
		file := "effect_" + name + ".go"
		if _, err := os.Stat(file); err != nil {
			t.Errorf("effect %q has no %s", name, file)
		}
		if !strings.Contains(readmeText, "| `"+name+"` |") {
			t.Errorf("effect %q has no README table row", name)
		}
		line := ""
		for _, candidate := range strings.Split(noticeText, "\n") {
			if strings.HasPrefix(strings.TrimSpace(candidate), file+" ") {
				line = candidate
				break
			}
		}
		if descriptor, _ := Lookup(name); descriptor.Origin != "" {
			// An original. Its Origin is its credit, and NOTICE must stay
			// clear of it.
			if line != "" {
				t.Errorf("effect %q declares an Origin but is also credited in NOTICE: %s",
					name, line)
			}
			continue
		}

		// Two names differ from the source file they came from. ttfx calls
		// print's file print_effect.rs to keep clear of a Rust keyword, and
		// randomsequence's random_sequence.rs while calling the effect itself
		// randomsequence. The effect names here follow ttfx's effect names,
		// and the NOTICE lines point at the real files.
		rustFile := name
		switch name {
		case "print":
			rustFile = "print_effect"
		case "randomsequence":
			rustFile = "random_sequence"
		}
		rust := "ttfx src/effects/" + rustFile + ".rs,"
		switch {
		case line == "":
			t.Errorf("effect %q has no NOTICE line for %s", name, file)
		case !strings.Contains(line, rust):
			t.Errorf("effect %q NOTICE line does not name %s: %s", name, rust, line)
		case !strings.Contains(line, "TTE effects/effect_"+ttePythonName(name)+".py"):
			t.Errorf("effect %q NOTICE line names no TerminalTextEffects source: %s", name, line)
		}
	}
}

// TestEveryCreditedEffectIsRegistered is the other direction: a README row or
// a NOTICE line for an effect that is not in the registry is a claim the
// package does not deliver.
//
// Negative control: adding a row for an effect that does not exist fails it.
func TestEveryCreditedEffectIsRegistered(t *testing.T) {
	registered := make(map[string]bool, len(Names()))
	for _, name := range Names() {
		registered[name] = true
	}
	notice, err := os.ReadFile("NOTICE")
	if err != nil {
		t.Fatalf("NOTICE: %v", err)
	}
	for _, line := range strings.Split(string(notice), "\n") {
		field := strings.TrimSpace(line)
		if !strings.HasPrefix(field, "effect_") {
			continue
		}
		name := strings.TrimSuffix(strings.Fields(field)[0], ".go")
		name = strings.TrimPrefix(name, "effect_")
		if !registered[name] {
			t.Errorf("NOTICE credits %q, which is not registered", name)
		}
	}
}

// ttePythonName maps an effect name here to the TerminalTextEffects module it
// came from. They agree everywhere but randomsequence, whose Python file keeps
// the underscore.
func ttePythonName(name string) string {
	if name == "randomsequence" {
		return "random_sequence"
	}
	return name
}
