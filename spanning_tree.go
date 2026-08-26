package tuiffects

import (
	"errors"
	"sort"
)

// Spanning-tree generators.
//
// Each one joins the characters of the canvas into a tree, one edge per Step,
// and records the order it did it in. An effect uses that order as its running
// order: burn lights characters in the order Prim's linked them, smoke rises
// through the same tree, laseretch cuts along a backtracker's path.
//
// They all share a shape:
//
//	generator, err := NewPrimsSimple(e, nil, true)
//	for !generator.Complete {
//	    generator.Step(e)
//	}
//	for _, ch := range generator.CharLinkOrder { ... }
//
// or, more often, one Step per frame so the tree grows on screen.
//
// The tree runs over the whole canvas, not just the input text, so an effect
// using one of these must set NeedsFillCharacters in its Descriptor. Without
// fill characters most of the canvas holds nothing, a random starting
// coordinate lands on an empty cell, and the constructor fails.
//
// Ported from ttfx src/utils/spanning_tree.rs. AldousBroder is not here
// because it is not there either: no shipped effect uses it.

// ErrNoStartingCharacter is returned when a generator picks a random starting
// coordinate and finds no character on it. It nearly always means the terminal
// was built without fill characters.
var ErrNoStartingCharacter = errors.New("tuiffects: no character at the spanning tree's starting coordinate")

// LinkCharacters joins two characters in both directions.
//
// Each side's Links stays sorted by character id, and linking a pair twice
// does nothing the second time. Upstream keeps a Python set here; the sort is
// what makes traversals of it repeatable.
//
// Upstream and ttfx both take the engine as an argument because their
// characters live in an arena. Here a character is a pointer, so it does not.
func LinkCharacters(a, b *Character) {
	insertLink(a, b)
	insertLink(b, a)
}

func insertLink(owner, link *Character) {
	i := sort.Search(len(owner.Links), func(i int) bool { return owner.Links[i].ID >= link.ID })
	if i < len(owner.Links) && owner.Links[i] == link {
		return
	}
	owner.Links = append(owner.Links, nil)
	copy(owner.Links[i+1:], owner.Links[i:])
	owner.Links[i] = link
}

// appendTreeNeighbors is upstream's get_neighbors: the four cardinal
// neighbours in north, east, south, west order, with the two optional filters.
//
// buf is a scratch buffer whose capacity is reused and whose contents are not.
// Pass nil or an emptied slice; anything already in it is overwritten.
func appendTreeNeighbors(buf []*Character, e *Engine, ch *Character, unlinkedOnly, limitToTextBoundary bool) []*Character {
	found := e.Terminal.appendNeighbors(buf[:0], ch)
	kept := found[:0]
	for _, neighbor := range found {
		if limitToTextBoundary && !e.Terminal.Canvas.CoordIsInText(neighbor.InputCoord) {
			continue
		}
		if unlinkedOnly && len(neighbor.Links) != 0 {
			continue
		}
		kept = append(kept, neighbor)
	}
	return kept
}

// startingCharacter resolves the optional starting character: a nil one means
// pick a random coordinate and take whatever stands on it.
func startingCharacter(e *Engine, given *Character, withinTextBoundary bool) (*Character, error) {
	if given != nil {
		return given, nil
	}
	coord := e.Terminal.Canvas.RandomCoord(e.Rng, false, withinTextBoundary)
	ch := e.Terminal.CharacterAtInputCoord(coord)
	if ch == nil {
		return nil, ErrNoStartingCharacter
	}
	return ch, nil
}

func removeAt(list []*Character, i int) (*Character, []*Character) {
	picked := list[i]
	copy(list[i:], list[i+1:])
	return picked, list[:len(list)-1]
}

// PrimsSimple grows a tree by repeatedly picking a random character off the
// edge of what it has already built and linking it to a random unlinked
// neighbour. The result wanders: it is what burn uses to spread a fire.
type PrimsSimple struct {
	// LimitToTextBoundary keeps the tree inside the text block rather than
	// letting it run out over the whole canvas.
	LimitToTextBoundary bool

	// CharLastLinked is the character the most recent Step joined to the
	// tree, or nil if that Step joined none.
	CharLastLinked *Character
	// CharLinkOrder is every character the tree has reached, starting one,
	// in the order it reached them. This is the order effects animate in.
	CharLinkOrder []*Character
	// EdgeChars are the characters still able to grow the tree.
	EdgeChars []*Character
	// EdgeLastAdded and EdgeLastPopped are the last character put on the edge
	// and the last one taken off it.
	EdgeLastAdded  *Character
	EdgeLastPopped *Character
	// Complete says the tree has stopped growing. See Step: it turns true one
	// Step after the last edge is used up, not on the same one.
	Complete bool

	currentChar *Character
	scratch     []*Character
}

// NewPrimsSimple starts a tree. A nil startingChar picks a random coordinate
// on the canvas, which needs fill characters to be reliable.
func NewPrimsSimple(e *Engine, startingChar *Character, limitToTextBoundary bool) (*PrimsSimple, error) {
	start, err := startingCharacter(e, startingChar, limitToTextBoundary)
	if err != nil {
		return nil, err
	}
	return &PrimsSimple{
		LimitToTextBoundary: limitToTextBoundary,
		CharLastLinked:      start,
		CharLinkOrder:       []*Character{start},
		EdgeChars:           []*Character{start},
		EdgeLastAdded:       start,
		currentChar:         start,
	}, nil
}

// Step grows the tree by at most one edge.
//
// Complete turns true on the Step that finds the edge already empty, not on
// the Step that empties it. That is upstream's behaviour and it costs one
// extra frame; an effect that loops until Complete gets one Step where nothing
// happens. Do not tighten it: the frame count of every effect built on this is
// tuned to it.
func (p *PrimsSimple) Step(e *Engine) {
	if len(p.EdgeChars) == 0 {
		p.Complete = true
		return
	}
	index := e.Rng.IntBelow(0, len(p.EdgeChars))
	p.currentChar, p.EdgeChars = removeAt(p.EdgeChars, index)
	p.EdgeLastPopped = p.currentChar

	unlinked := appendTreeNeighbors(p.scratch[:0], e, p.currentChar, true, p.LimitToTextBoundary)
	p.scratch = unlinked
	if len(unlinked) == 0 {
		return
	}
	var next *Character
	next, unlinked = removeAt(unlinked, e.Rng.IntBelow(0, len(unlinked)))
	LinkCharacters(p.currentChar, next)
	p.CharLinkOrder = append(p.CharLinkOrder, next)
	p.CharLastLinked = next
	// The remaining list was taken before the link was made, so it can hold
	// characters that are now linked. Upstream only asks whether it is empty,
	// and so does this.
	if len(unlinked) != 0 {
		p.EdgeChars = append(p.EdgeChars, p.currentChar)
	}
	if len(appendTreeNeighbors(nil, e, next, true, p.LimitToTextBoundary)) != 0 {
		p.EdgeChars = append(p.EdgeChars, next)
		p.EdgeLastAdded = next
	}
}

// WeightedLink is one candidate edge and the weight it was drawn with.
type WeightedLink struct {
	CharA  *Character
	CharB  *Character
	Weight int
}

// PrimsWeighted gives every character a random weight once, then always grows
// towards the cheapest character on the frontier. The tree it builds looks
// less like a random walk and more like something seeping outwards, which is
// why smoke uses it.
type PrimsWeighted struct {
	// LimitToTextBoundary keeps the tree inside the text block.
	LimitToTextBoundary bool

	// CharLastLinked is the character the most recent Step joined, or nil
	// once the tree is finished.
	CharLastLinked *Character
	// CharLinkOrder is every character the tree has reached, in order.
	CharLinkOrder []*Character
	// NeighborsLastAdded are the characters the most recent Step offered up
	// as new candidates.
	NeighborsLastAdded []*Character
	// Complete says the tree has stopped growing.
	Complete bool

	// charWeights is indexed by the character's slot in Terminal.Characters.
	charWeights []int
	// pending holds the candidate edges by weight, with pendingWeights
	// keeping the weights that are in use sorted so the lowest is at the
	// front. Upstream uses an ordered map for the same reason.
	pending        map[int][]WeightedLink
	pendingWeights []int
}

// NewPrimsWeighted starts a tree. A nil startingChar picks a random
// coordinate.
func NewPrimsWeighted(e *Engine, startingChar *Character, limitToTextBoundary bool) (*PrimsWeighted, error) {
	start, err := startingCharacter(e, startingChar, limitToTextBoundary)
	if err != nil {
		return nil, err
	}
	p := &PrimsWeighted{
		LimitToTextBoundary: limitToTextBoundary,
		CharLastLinked:      start,
		CharLinkOrder:       []*Character{start},
		charWeights:         make([]int, len(e.Terminal.Characters)),
		pending:             make(map[int][]WeightedLink),
	}
	// Weights are drawn over the input and fill characters in reading order.
	// That order is the order the random draws happen in, so it is part of
	// what the effect looks like, not just bookkeeping.
	ordered := e.Terminal.GetCharacters(e.Rng,
		CharacterFilter{Input: true, InnerFill: true, OuterFill: true},
		SortTopToBottomLeftToRight)
	for _, ch := range ordered {
		p.charWeights[ch.index] = e.Rng.IntBetween(0, 99)
	}
	p.addWeightedLinks(e, start)
	return p, nil
}

func (p *PrimsWeighted) weightOf(ch *Character) int {
	if ch.index < 0 || ch.index >= len(p.charWeights) {
		return 0
	}
	return p.charWeights[ch.index]
}

func (p *PrimsWeighted) addWeightedLinks(e *Engine, ch *Character) {
	p.NeighborsLastAdded = p.NeighborsLastAdded[:0]
	for _, neighbor := range appendTreeNeighbors(nil, e, ch, true, p.LimitToTextBoundary) {
		p.NeighborsLastAdded = append(p.NeighborsLastAdded, neighbor)
		weight := p.weightOf(neighbor)
		if _, seen := p.pending[weight]; !seen {
			i := sort.SearchInts(p.pendingWeights, weight)
			p.pendingWeights = append(p.pendingWeights, 0)
			copy(p.pendingWeights[i+1:], p.pendingWeights[i:])
			p.pendingWeights[i] = weight
		}
		p.pending[weight] = append(p.pending[weight], WeightedLink{CharA: ch, CharB: neighbor, Weight: weight})
	}
}

// lowestWeightLink pulls candidates off the cheapest weight until it finds one
// whose far end is still unlinked, or runs out.
func (p *PrimsWeighted) lowestWeightLink(e *Engine) (WeightedLink, bool) {
	for len(p.pendingWeights) != 0 {
		weight := p.pendingWeights[0]
		links := p.pending[weight]
		if len(links) == 0 {
			delete(p.pending, weight)
			p.pendingWeights = p.pendingWeights[1:]
			continue
		}
		index := e.Rng.IntBelow(0, len(links))
		link := links[index]
		copy(links[index:], links[index+1:])
		links = links[:len(links)-1]
		if len(links) == 0 {
			delete(p.pending, weight)
			p.pendingWeights = p.pendingWeights[1:]
		} else {
			p.pending[weight] = links
		}
		if len(link.CharB.Links) == 0 {
			return link, true
		}
	}
	return WeightedLink{}, false
}

// Step grows the tree by one edge, taking the cheapest one available.
func (p *PrimsWeighted) Step(e *Engine) {
	if len(p.pendingWeights) == 0 {
		p.Complete = true
		p.CharLastLinked = nil
		p.NeighborsLastAdded = p.NeighborsLastAdded[:0]
		return
	}
	link, ok := p.lowestWeightLink(e)
	if !ok {
		p.Complete = true
		return
	}
	LinkCharacters(link.CharA, link.CharB)
	p.CharLastLinked = link.CharB
	p.CharLinkOrder = append(p.CharLinkOrder, link.CharB)
	p.addWeightedLinks(e, link.CharB)
}

// RecursiveBacktracker walks as far as it can in one direction, then backs up
// to the last character with somewhere left to go. The tree it makes is long
// corridors rather than a spreading blob, which is what laseretch cuts along.
type RecursiveBacktracker struct {
	// LimitToTextBoundary keeps the walk inside the text block.
	LimitToTextBoundary bool

	// CharLastLinked is the character this Step joined, or nil if this Step
	// backtracked instead of moving forward.
	CharLastLinked *Character
	// CharLinkOrder is every character reached, in order.
	CharLinkOrder []*Character
	// Stack is the route back to the last character with an unvisited
	// neighbour.
	Stack []*Character
	// StackLastPopped is the character this Step backtracked off the stack,
	// or nil if this Step moved forward instead.
	StackLastPopped *Character
	// Complete says the walk has stopped.
	Complete bool

	currentChar *Character
	scratch     []*Character
}

// NewRecursiveBacktracker starts a walk. A nil startingChar picks a random
// coordinate.
func NewRecursiveBacktracker(e *Engine, startingChar *Character, limitToTextBoundary bool) (*RecursiveBacktracker, error) {
	start, err := startingCharacter(e, startingChar, limitToTextBoundary)
	if err != nil {
		return nil, err
	}
	return &RecursiveBacktracker{
		LimitToTextBoundary: limitToTextBoundary,
		CharLastLinked:      start,
		CharLinkOrder:       []*Character{start},
		Stack:               []*Character{start},
		currentChar:         start,
	}, nil
}

// Step either moves forward into an unvisited neighbour or backs up one.
// Exactly one of CharLastLinked and StackLastPopped is set afterwards, and
// both are cleared first, so an effect can tell which of the two happened.
func (r *RecursiveBacktracker) Step(e *Engine) {
	r.CharLastLinked = nil
	r.StackLastPopped = nil
	if len(r.Stack) == 0 {
		r.Complete = true
		return
	}
	unvisited := appendTreeNeighbors(r.scratch[:0], e, r.currentChar, true, r.LimitToTextBoundary)
	r.scratch = unvisited
	if len(unvisited) == 0 {
		r.StackLastPopped = r.Stack[len(r.Stack)-1]
		r.Stack = r.Stack[:len(r.Stack)-1]
		if len(r.Stack) != 0 {
			r.currentChar = r.Stack[len(r.Stack)-1]
		}
		return
	}
	next := *Choice(e.Rng, unvisited)
	LinkCharacters(r.currentChar, next)
	r.CharLinkOrder = append(r.CharLinkOrder, next)
	r.CharLastLinked = next
	r.Stack = append(r.Stack, next)
	r.currentChar = next
}

// BreadthFirst walks a tree that one of the generators above has already
// built, one whole layer per Step. It builds nothing itself and draws no
// random numbers, so it is what an effect uses to sweep outwards from a point
// along links that are already there.
type BreadthFirst struct {
	// StartingChar is where the sweep began.
	StartingChar *Character
	// ExploredLastStep are the characters this Step reached. This is the
	// layer, and it is what an effect animates.
	ExploredLastStep []*Character
	// CharExploreOrder is every character reached so far, in order. It does
	// not include the starting character.
	CharExploreOrder []*Character
	// Complete says the sweep ran out of frontier.
	Complete bool

	frontier    []*Character
	inFrontier  map[*Character]struct{}
	explored    map[*Character]struct{}
	newEdgeSeen map[*Character]struct{}
}

// NewBreadthFirst starts a sweep. A nil startingChar picks a random
// coordinate. The links it follows must already exist: run one of the tree
// generators to completion first.
func NewBreadthFirst(e *Engine, startingChar *Character, limitToTextBoundary bool) (*BreadthFirst, error) {
	start, err := startingCharacter(e, startingChar, limitToTextBoundary)
	if err != nil {
		return nil, err
	}
	return &BreadthFirst{
		StartingChar: start,
		frontier:     []*Character{start},
		inFrontier:   map[*Character]struct{}{start: {}},
		explored:     map[*Character]struct{}{start: {}},
		newEdgeSeen:  map[*Character]struct{}{},
	}, nil
}

// Step drains the whole frontier and makes everything it reached the next one.
// One Step is one layer, however wide that layer is.
func (b *BreadthFirst) Step() {
	b.ExploredLastStep = b.ExploredLastStep[:0]
	if len(b.frontier) == 0 {
		b.Complete = true
		return
	}
	var newEdges []*Character
	clear(b.newEdgeSeen)
	for len(b.frontier) != 0 {
		position := b.frontier[0]
		b.frontier = b.frontier[1:]
		delete(b.inFrontier, position)
		for _, link := range position.Links {
			if _, seen := b.explored[link]; seen {
				continue
			}
			if _, seen := b.inFrontier[link]; seen {
				continue
			}
			if _, seen := b.newEdgeSeen[link]; seen {
				continue
			}
			b.newEdgeSeen[link] = struct{}{}
			b.explored[link] = struct{}{}
			b.ExploredLastStep = append(b.ExploredLastStep, link)
			b.CharExploreOrder = append(b.CharExploreOrder, link)
			newEdges = append(newEdges, link)
		}
	}
	b.frontier = newEdges
	for _, ch := range newEdges {
		b.inFrontier[ch] = struct{}{}
	}
}
