package tuiffects

// Character is one cell of the input, with everything needed to animate it:
// where it came from, where it is now, what it looks like, and what it does
// when its motion or animation reaches a milestone.
type Character struct {
	// ID is allocation order. It is the canonical ordering key everywhere,
	// because effects that walk characters must walk them the same way twice.
	ID int

	InputSymbol string
	InputCoord  Coord

	IsVisible bool
	Layer     int

	Animation Animation
	Motion    Motion

	// UsesInputColors marks a character that carried its own colours in, which
	// the input-colour policies key off.
	UsesInputColors bool
	// IsFill marks a character the engine invented to pad the canvas rather
	// than one that came from the input.
	IsFill bool

	// Links are the characters this one has been joined to by a spanning
	// tree, kept in ascending id order. Upstream holds a Python set here and
	// several effects walk it, so the order is pinned rather than left to
	// whatever a set happens to yield. LinkCharacters is the only thing that
	// writes it.
	Links []*Character

	handler eventHandler

	// index is the slot in Terminal.Characters. Kept so a character can find
	// itself without a search.
	index int
}

func newCharacter(id int, symbol string, coord Coord, visuals *visualCache) *Character {
	return &Character{
		ID:          id,
		InputSymbol: symbol,
		InputCoord:  coord,
		Animation:   newAnimation(symbol, visuals),
		Motion:      newMotion(coord),
	}
}

// IsActive reports whether the character still has work to do. Upstream counts
// a looping scene as complete, so a character that only loops reads as
// inactive; that quirk is kept because effects rely on it to decide when they
// have finished.
func (c *Character) IsActive() bool {
	return !c.Motion.MovementIsComplete() || !c.Animation.ActiveSceneIsComplete()
}

// RegisterEvent hangs an action off an event raised by a scene, path or
// waypoint on this character.
func (c *Character) RegisterEvent(event Event, from Caller, action Action) {
	c.handler.register(event, from, action)
}

// ClearEvents drops every action registered on this character. A particle
// pool calls it when a reused particle must not still be carrying the
// handlers its previous life registered.
func (c *Character) ClearEvents() {
	c.handler.actions = nil
	c.handler.subscribed = 0
}
