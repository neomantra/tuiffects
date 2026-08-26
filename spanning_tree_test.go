package tuiffects

import (
	"errors"
	"strings"
	"testing"
)

// gridEngine builds an engine whose canvas is entirely input characters, so
// every cell has a character and the text block is the whole canvas.
func gridEngine(t *testing.T, width, height, seed int) *Engine {
	t.Helper()
	lines := make([]string, height)
	for i := range lines {
		lines[i] = strings.Repeat("#", width)
	}
	terminal := NewTerminalFromText(strings.Join(lines, "\n"), TerminalConfig{Width: width, Height: height})
	if got := len(terminal.Characters); got != width*height {
		t.Fatalf("built %d characters for a %dx%d grid, want %d", got, width, height, width*height)
	}
	return NewEngine(terminal, NewRng(uint64(seed)))
}

// treeShape walks the links from one character and reports how many characters
// it reached and how many distinct edges it crossed.
func treeShape(start *Character) (reached, edges int) {
	seen := map[*Character]bool{start: true}
	queue := []*Character{start}
	for len(queue) > 0 {
		ch := queue[0]
		queue = queue[1:]
		for _, link := range ch.Links {
			edges++
			if !seen[link] {
				seen[link] = true
				queue = append(queue, link)
			}
		}
	}
	// Every edge was counted from both ends.
	return len(seen), edges / 2
}

// assertSpanningTree checks the property the effects actually rely on: every
// character joined, joined once, with no cycle.
func assertSpanningTree(t *testing.T, e *Engine, order []*Character) {
	t.Helper()
	want := len(e.Terminal.Characters)
	if len(order) != want {
		t.Fatalf("the link order holds %d characters, want all %d", len(order), want)
	}
	seen := map[*Character]bool{}
	for _, ch := range order {
		if seen[ch] {
			t.Fatalf("character %d is in the link order twice", ch.ID)
		}
		seen[ch] = true
	}
	reached, edges := treeShape(order[0])
	if reached != want {
		t.Errorf("walking the links reaches %d characters, want %d, so the tree is not connected", reached, want)
	}
	if edges != want-1 {
		t.Errorf("the tree has %d edges over %d characters, want %d, so it holds a cycle", edges, want, want-1)
	}
}

// TestNeighborsAreCardinalAndOrdered pins the four slots and their order.
// Every generator draws a random index into this list, so the order is part of
// what the effect looks like, not just an implementation detail.
//
// Negative control: adding the four diagonals to Terminal.appendNeighbors
// makes the centre report eight neighbours and this fails. Confirmed failing.
func TestNeighborsAreCardinalAndOrdered(t *testing.T) {
	e := gridEngine(t, 3, 3, 1)
	centre := e.Terminal.CharacterAtInputCoord(C(2, 2))
	if centre == nil {
		t.Fatal("no character at the centre of the grid")
	}
	neighbors := e.Terminal.Neighbors(centre)
	if len(neighbors) != 4 {
		t.Fatalf("the centre has %d neighbours, want 4", len(neighbors))
	}
	want := []Coord{C(2, 3), C(3, 2), C(2, 1), C(1, 2)}
	for i, coord := range want {
		if neighbors[i].InputCoord != coord {
			t.Errorf("neighbour %d is at %v, want %v (north, east, south, west)", i, neighbors[i].InputCoord, coord)
		}
	}
	corner := e.Terminal.CharacterAtInputCoord(C(1, 1))
	if got := len(e.Terminal.Neighbors(corner)); got != 2 {
		t.Errorf("the bottom left corner has %d neighbours, want 2", got)
	}
}

// TestLinkCharactersIsMutualSortedAndIdempotent covers all three properties
// the tree walks depend on.
//
// Negative control: dropping the second insertLink call leaves the far side
// empty and this fails. Confirmed failing.
func TestLinkCharactersIsMutualSortedAndIdempotent(t *testing.T) {
	e := gridEngine(t, 3, 1, 1)
	a, b, c := e.Terminal.Characters[0], e.Terminal.Characters[1], e.Terminal.Characters[2]

	LinkCharacters(b, c)
	LinkCharacters(b, a)
	if len(a.Links) != 1 || a.Links[0] != b {
		t.Errorf("a has %d links, want one to b", len(a.Links))
	}
	if len(c.Links) != 1 || c.Links[0] != b {
		t.Errorf("c has %d links, want one to b", len(c.Links))
	}
	if len(b.Links) != 2 {
		t.Fatalf("b has %d links, want 2", len(b.Links))
	}
	if b.Links[0].ID > b.Links[1].ID {
		t.Errorf("b's links are in ids %d then %d, want ascending", b.Links[0].ID, b.Links[1].ID)
	}

	LinkCharacters(b, a)
	LinkCharacters(a, b)
	if len(b.Links) != 2 || len(a.Links) != 1 {
		t.Errorf("linking the same pair again gave b %d links and a %d, want 2 and 1", len(b.Links), len(a.Links))
	}
}

// TestPrimsSimpleBuildsASpanningTree is the property burn, smoke and laseretch
// all rest on: every character joined exactly once, connected, no cycle.
//
// Negative control: dropping the "push the current character back when it has
// unlinked neighbours left" branch strands cells and this fails on the reach
// count. Confirmed failing.
func TestPrimsSimpleBuildsASpanningTree(t *testing.T) {
	e := gridEngine(t, 9, 6, 7)
	tree, err := NewPrimsSimple(e, nil, false)
	if err != nil {
		t.Fatalf("building the tree: %v", err)
	}
	for steps := 0; !tree.Complete; steps++ {
		if steps > 10000 {
			t.Fatal("the tree never reported itself complete")
		}
		tree.Step(e)
	}
	assertSpanningTree(t, e, tree.CharLinkOrder)
}

// TestPrimsSimpleReportsCompleteOneStepLate pins the quirk in Step.
//
// Complete turns true on the step that finds the edge list already empty, not
// on the step that empties it, so there is always one final step that links
// nothing. Every effect built on this counts frames, so the extra step is part
// of how long the effect runs.
//
// Negative control: reporting Complete at the end of any step that leaves the
// edge list empty removes that step and this fails. Confirmed failing.
func TestPrimsSimpleReportsCompleteOneStepLate(t *testing.T) {
	e := gridEngine(t, 5, 4, 3)
	tree, err := NewPrimsSimple(e, nil, false)
	if err != nil {
		t.Fatalf("building the tree: %v", err)
	}
	emptiedAt := -1
	for steps := 0; !tree.Complete; steps++ {
		if steps > 10000 {
			t.Fatal("the tree never reported itself complete")
		}
		tree.Step(e)
		if len(tree.EdgeChars) == 0 && emptiedAt < 0 {
			emptiedAt = steps
			if tree.Complete {
				t.Fatal("Complete turned true on the step that emptied the edge list, one step early")
			}
		}
	}
	if emptiedAt < 0 {
		t.Fatal("the edge list never emptied")
	}
	linkedBefore := len(tree.CharLinkOrder)
	tree.Step(e)
	if len(tree.CharLinkOrder) != linkedBefore {
		t.Error("a step after Complete linked another character")
	}
}

// TestPrimsWeightedBuildsASpanningTree covers the generator smoke uses.
//
// Negative control: skipping the "is the far end still unlinked" check in
// lowestWeightLink lets a link close a cycle and the edge count fails.
// Confirmed failing.
func TestPrimsWeightedBuildsASpanningTree(t *testing.T) {
	e := gridEngine(t, 8, 5, 11)
	tree, err := NewPrimsWeighted(e, nil, false)
	if err != nil {
		t.Fatalf("building the tree: %v", err)
	}
	for steps := 0; !tree.Complete; steps++ {
		if steps > 10000 {
			t.Fatal("the tree never reported itself complete")
		}
		tree.Step(e)
	}
	assertSpanningTree(t, e, tree.CharLinkOrder)
	linked := len(tree.CharLinkOrder)
	tree.Step(e)
	if len(tree.CharLinkOrder) != linked {
		t.Error("a step after Complete linked another character")
	}
}

// TestPrimsWeightedTakesTheCheapestEdgeFirst checks the thing the generator is
// named for: of the edges on offer, the one to the lowest-weight character
// wins. Without it this is just a slower PrimsSimple and smoke loses its
// seeping look.
//
// "On offer" means an edge whose far end is still unlinked. The cheapest
// bucket often holds stale edges to characters some other edge already
// reached, and the generator drops those and moves up, so the weight it takes
// is not always the lowest weight still in the table.
//
// Negative control: taking the highest live weight instead of the lowest fails
// on the first step. Confirmed failing.
func TestPrimsWeightedTakesTheCheapestEdgeFirst(t *testing.T) {
	e := gridEngine(t, 7, 5, 5)
	start := e.Terminal.CharacterAtInputCoord(C(4, 3))
	tree, err := NewPrimsWeighted(e, start, false)
	if err != nil {
		t.Fatalf("building the tree: %v", err)
	}
	checked := 0
	for steps := 0; steps < 20 && !tree.Complete; steps++ {
		cheapest := -1
		for weight, links := range tree.pending {
			for _, link := range links {
				if len(link.CharB.Links) == 0 && (cheapest < 0 || weight < cheapest) {
					cheapest = weight
				}
			}
		}
		tree.Step(e)
		if tree.CharLastLinked == nil {
			continue
		}
		if got := tree.weightOf(tree.CharLastLinked); got != cheapest {
			t.Fatalf("step %d linked a character of weight %d while weight %d was on offer", steps, got, cheapest)
		}
		checked++
	}
	if checked < 10 {
		t.Fatalf("only %d steps linked anything, too few to prove the ordering", checked)
	}
}

// TestRecursiveBacktrackerBuildsASpanningTreeAndBacktracks covers the
// generator laseretch cuts along, and the two-outcome contract of its Step:
// each step either moves forward or backs up, never both and never neither.
//
// Negative control: clearing the stack instead of popping one entry makes the
// walk finish after the first dead end, so the tree covers only part of the
// canvas and the reach count fails. Confirmed failing.
func TestRecursiveBacktrackerBuildsASpanningTreeAndBacktracks(t *testing.T) {
	e := gridEngine(t, 8, 5, 13)
	walk, err := NewRecursiveBacktracker(e, nil, false)
	if err != nil {
		t.Fatalf("building the walk: %v", err)
	}
	backtracks := 0
	for steps := 0; !walk.Complete; steps++ {
		if steps > 10000 {
			t.Fatal("the walk never reported itself complete")
		}
		walk.Step(e)
		if walk.Complete {
			break
		}
		if walk.CharLastLinked != nil && walk.StackLastPopped != nil {
			t.Fatalf("step %d both linked a character and backtracked", steps)
		}
		if walk.CharLastLinked == nil && walk.StackLastPopped == nil {
			t.Fatalf("step %d neither linked a character nor backtracked", steps)
		}
		if walk.StackLastPopped != nil {
			backtracks++
		}
	}
	if backtracks == 0 {
		t.Error("the walk never backtracked, so it is not a backtracker")
	}
	assertSpanningTree(t, e, walk.CharLinkOrder)
}

// TestBreadthFirstExploresOneLayerPerStep checks the thing BreadthFirst is
// named for. It walks links another generator already made, and each step must
// hand back exactly the ring of characters at the next distance from the
// start. An effect uses that ring as one frame, so a step that returned two
// rings at once, or one character at a time, would change the shape on screen.
//
// The tree here is built by hand so the rings are known: a plus, with the
// centre linked to four arms of two characters each.
//
//	    e
//	    c
//	f d a b g
//	    h
//	    i
//
// Negative control: exploring one character per step instead of one ring, the
// way a depth-first walk would, gives a first step of one character and this
// fails. Confirmed failing.
func TestBreadthFirstExploresOneLayerPerStep(t *testing.T) {
	e := gridEngine(t, 5, 5, 1)
	at := func(column, row int) *Character { return e.Terminal.CharacterAtInputCoord(C(column, row)) }
	centre := at(3, 3)
	arms := [][2]*Character{
		{at(4, 3), at(5, 3)},
		{at(2, 3), at(1, 3)},
		{at(3, 4), at(3, 5)},
		{at(3, 2), at(3, 1)},
	}
	inner := map[*Character]bool{}
	outer := map[*Character]bool{}
	for _, arm := range arms {
		LinkCharacters(centre, arm[0])
		LinkCharacters(arm[0], arm[1])
		inner[arm[0]] = true
		outer[arm[1]] = true
	}

	sweep, err := NewBreadthFirst(e, centre, false)
	if err != nil {
		t.Fatalf("building the sweep: %v", err)
	}

	sweep.Step()
	if len(sweep.ExploredLastStep) != 4 {
		t.Fatalf("the first ring holds %d characters, want the 4 next to the centre", len(sweep.ExploredLastStep))
	}
	for _, ch := range sweep.ExploredLastStep {
		if !inner[ch] {
			t.Errorf("%v is in the first ring but is not next to the centre", ch.InputCoord)
		}
	}

	sweep.Step()
	if len(sweep.ExploredLastStep) != 4 {
		t.Fatalf("the second ring holds %d characters, want the 4 arm tips", len(sweep.ExploredLastStep))
	}
	for _, ch := range sweep.ExploredLastStep {
		if !outer[ch] {
			t.Errorf("%v is in the second ring but is not an arm tip", ch.InputCoord)
		}
	}

	// The tips are still the frontier, so one more step drains them and finds
	// nothing new, and only the step after that reports Complete. This is the
	// same one-step-late finish PrimsSimple has, and effects count frames.
	sweep.Step()
	if len(sweep.ExploredLastStep) != 0 {
		t.Errorf("after the arms ran out the sweep reported %d more characters", len(sweep.ExploredLastStep))
	}
	if sweep.Complete {
		t.Error("Complete turned true on the step that emptied the frontier, one step early")
	}
	sweep.Step()
	if !sweep.Complete {
		t.Error("the sweep never reported itself complete")
	}
	if len(sweep.CharExploreOrder) != 8 {
		t.Errorf("the sweep reached %d characters, want 8; the starting character is not one of them",
			len(sweep.CharExploreOrder))
	}
}

// TestSpanningTreeLimitToTextBoundary checks the flag every effect passes.
//
// Negative control: ignoring limitToTextBoundary in appendTreeNeighbors lets
// the tree run out over the fill characters and this fails. Confirmed failing.
func TestSpanningTreeLimitToTextBoundary(t *testing.T) {
	terminal := NewTerminalFromText("####\n####\n####", TerminalConfig{
		Width: 20, Height: 12, AnchorText: AnchorC, MakeFillCharacters: true,
	})
	e := NewEngine(terminal, NewRng(21))
	textCells := terminal.Canvas.TextWidth * terminal.Canvas.TextHeight
	if textCells >= len(terminal.Characters) {
		t.Fatalf("the text covers %d of %d cells, so the test would prove nothing", textCells, len(terminal.Characters))
	}

	tree, err := NewPrimsSimple(e, nil, true)
	if err != nil {
		t.Fatalf("building the tree: %v", err)
	}
	for steps := 0; !tree.Complete; steps++ {
		if steps > 10000 {
			t.Fatal("the tree never reported itself complete")
		}
		tree.Step(e)
	}
	for _, ch := range tree.CharLinkOrder {
		if !terminal.Canvas.CoordIsInText(ch.InputCoord) {
			t.Fatalf("the tree reached %v, which is outside the text block", ch.InputCoord)
		}
	}
	if len(tree.CharLinkOrder) != textCells {
		t.Errorf("the tree covered %d cells, want the %d in the text block", len(tree.CharLinkOrder), textCells)
	}
}

// TestSpanningTreeNeedsFillCharacters documents the failure a porter is most
// likely to hit: a generator picks a random canvas coordinate to start from,
// and on a canvas with no fill characters most coordinates hold nothing.
//
// Negative control: with MakeFillCharacters set, the same seed and the same
// canvas succeed. That control is run here as the second half of the test, so
// the first half cannot be passing for the wrong reason.
func TestSpanningTreeNeedsFillCharacters(t *testing.T) {
	const seed = 4
	cfg := TerminalConfig{Width: 40, Height: 20, AnchorText: AnchorC}

	bare := NewTerminalFromText("##\n##", cfg)
	if _, err := NewPrimsSimple(NewEngine(bare, NewRng(seed)), nil, false); !errors.Is(err, ErrNoStartingCharacter) {
		t.Fatalf("on a canvas with no fill characters the tree started with err=%v, want ErrNoStartingCharacter", err)
	}

	cfg.MakeFillCharacters = true
	filled := NewTerminalFromText("##\n##", cfg)
	if _, err := NewPrimsSimple(NewEngine(filled, NewRng(seed)), nil, false); err != nil {
		t.Fatalf("with fill characters the same seed failed: %v", err)
	}
}
