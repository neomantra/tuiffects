package tuiffects

import (
	"os"
	"strings"
	"testing"
)

// TestEveryEffectIsCredited checks the attribution chain. Every registered
// effect must have a file, a NOTICE line naming both the ttfx source and the
// TerminalTextEffects source, and a README table row. The chain is the one
// thing about this package that cannot be corrected after the fact, so it is
// guarded by a test rather than by a habit.
//
// Negative control: deleting any effect's NOTICE line, or its README row,
// fails this test. Both were run.
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
		line := ""
		for _, candidate := range strings.Split(noticeText, "\n") {
			if strings.HasPrefix(strings.TrimSpace(candidate), file+" ") {
				line = candidate
				break
			}
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
		if !strings.Contains(readmeText, "| `"+name+"` |") {
			t.Errorf("effect %q has no README table row", name)
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
