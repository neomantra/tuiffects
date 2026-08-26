// Package tuiffects animates terminal text.
//
// It is a Go port of ttfx, which is itself a port of TerminalTextEffects by
// ChrisBuilds. Every effect here, and the architecture of the engine that runs
// them, are that project's design; this package originates none of the art.
// See NOTICE for the full chain and LICENSE for the three copyrights it
// carries.
package tuiffects

import "math"

// Coord is a 1-based canvas coordinate. Column grows right and row grows up,
// so the origin sits at the bottom left, as it does upstream.
type Coord struct {
	Column int
	Row    int
}

// C builds a Coord. It exists because effect code writes thousands of them.
func C(column, row int) Coord { return Coord{Column: column, Row: row} }

// roundHalfEven matches Python's round(), which ttfx calls banker's rounding.
// Coordinates land on it everywhere, so a plain round would shift paths by a
// cell in about half the ties.
func roundHalfEven(v float64) int { return int(math.RoundToEven(v)) }

// floorDiv is Python's // on ints: it rounds towards negative infinity, unlike
// Go's / which truncates towards zero.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// floatPoint is a bezier intermediate. Upstream builds Coords with float
// fields inside De Casteljau and only rounds the result, so the port keeps a
// separate type rather than rounding at every level.
type floatPoint struct {
	column float64
	row    float64
}

func (p floatPoint) interpolate(other floatPoint, t float64) floatPoint {
	return floatPoint{
		column: (1-t)*p.column + t*other.column,
		row:    (1-t)*p.row + t*other.row,
	}
}

// FindLengthOfLine returns the distance between two coords. Terminal cells are
// about twice as tall as they are wide, so callers that want visually even
// motion pass doubleRowDiff.
func FindLengthOfLine(a, b Coord, doubleRowDiff bool) float64 {
	columnDiff := float64(b.Column - a.Column)
	rowDiff := float64(b.Row - a.Row)
	if doubleRowDiff {
		return math.Hypot(columnDiff, 2*rowDiff)
	}
	return math.Hypot(columnDiff, rowDiff)
}

// FindCoordOnLine interpolates between two coords and rounds the result.
func FindCoordOnLine(start, end Coord, t float64) Coord {
	x := (1-t)*float64(start.Column) + t*float64(end.Column)
	y := (1-t)*float64(start.Row) + t*float64(end.Row)
	return C(roundHalfEven(x), roundHalfEven(y))
}

// FindCoordOnBezierCurve runs De Casteljau over any number of control points.
func FindCoordOnBezierCurve(start Coord, control []Coord, end Coord, t float64) Coord {
	if len(control) == 0 {
		return FindCoordOnLine(start, end, t)
	}
	points := make([]floatPoint, 0, len(control)+2)
	points = append(points, floatPoint{float64(start.Column), float64(start.Row)})
	for _, c := range control {
		points = append(points, floatPoint{float64(c.Column), float64(c.Row)})
	}
	points = append(points, floatPoint{float64(end.Column), float64(end.Row)})
	for remaining := len(points); remaining > 1; remaining-- {
		for i := 0; i < remaining-1; i++ {
			points[i] = points[i].interpolate(points[i+1], t)
		}
	}
	return C(roundHalfEven(points[0].column), roundHalfEven(points[0].row))
}

// FindLengthOfBezierCurve estimates arc length from a 10-sample polyline.
//
// The walk stops at t=0.9 and never adds the last tenth of the curve, so every
// length comes out short. That is upstream behaviour. It is kept because path
// speed divides by this number, and correcting it would quietly make every
// curved path in every effect finish sooner than its author tuned it to.
func FindLengthOfBezierCurve(start Coord, control []Coord, end Coord) float64 {
	length := 0.0
	prev := start
	for t := 1; t < 10; t++ {
		coord := FindCoordOnBezierCurve(start, control, end, float64(t)/10)
		length += FindLengthOfLine(prev, coord, true)
		prev = coord
	}
	return length
}

// FindCoordsOnCircle returns points on a circle around origin. The column
// offset is doubled so the circle looks round in a terminal cell grid.
func FindCoordsOnCircle(origin Coord, radius, coordsLimit int, unique bool) []Coord {
	if radius == 0 {
		return nil
	}
	if coordsLimit == 0 {
		coordsLimit = roundHalfEven(2 * math.Pi * float64(radius))
	}
	if coordsLimit <= 0 {
		return nil
	}
	seen := make(map[Coord]struct{}, coordsLimit)
	points := make([]Coord, 0, coordsLimit)
	angleStep := 2 * math.Pi / float64(coordsLimit)
	for i := 0; i < coordsLimit; i++ {
		angle := angleStep * float64(i)
		x := float64(origin.Column) + float64(radius)*math.Cos(angle)
		x += x - float64(origin.Column)
		y := float64(origin.Row) + float64(radius)*math.Sin(angle)
		point := C(roundHalfEven(x), roundHalfEven(y))
		if _, ok := seen[point]; !unique || !ok {
			points = append(points, point)
		}
		seen[point] = struct{}{}
	}
	return points
}

// FindNormalizedDistanceFromCenter reports how far a coord sits from the
// centre of a rectangle, from 0 at the centre to 1 at a corner. It returns
// false when the coord is outside the rectangle.
func FindNormalizedDistanceFromCenter(bottom, top, left, right int, other Coord) (float64, bool) {
	rowOffset := bottom - 1
	columnOffset := left - 1
	right -= columnOffset
	top -= rowOffset
	centerX := float64(right) / 2
	centerY := float64(top) / 2

	column := other.Column - columnOffset
	row := other.Row - rowOffset
	if column < left-columnOffset || column > right || row < bottom-rowOffset || row > top {
		return 0, false
	}

	maxDistance := math.Sqrt(float64(right)*float64(right) + float64(top*2)*float64(top*2))
	dx := float64(column) - centerX
	dy := (float64(row) - centerY) * 2
	distance := math.Sqrt(dx*dx + dy*dy)
	return distance / (maxDistance / 2), true
}

// FindCoordsInCircle returns every coordinate inside an ellipse centred on the
// given coord.
//
// Upstream calls this a circle and builds an ellipse: the horizontal
// semi-axis is the diameter and the vertical one is half of it. That is the
// terminal cell aspect correction done in the shape rather than in the
// distance, and effects are tuned to the shape it actually produces.
func FindCoordsInCircle(center Coord, diameter int) []Coord {
	if diameter == 0 {
		return nil
	}
	var coords []Coord
	aSquared := float64(diameter) * float64(diameter)
	bSquared := (float64(diameter) / 2) * (float64(diameter) / 2)
	for x := center.Column - diameter; x <= center.Column+diameter; x++ {
		dx := float64(x - center.Column)
		// int() truncation, as upstream does it.
		maxYOffset := int(math.Sqrt(bSquared * (1 - (dx*dx)/aSquared)))
		for y := center.Row - maxYOffset; y <= center.Row+maxYOffset; y++ {
			coords = append(coords, C(x, y))
		}
	}
	return coords
}

// FindCoordsInRect returns every coordinate in the square block reaching
// distance cells out from the origin. A distance of zero returns nothing.
func FindCoordsInRect(origin Coord, distance int) []Coord {
	if distance == 0 {
		return nil
	}
	var coords []Coord
	for column := origin.Column - distance; column <= origin.Column+distance; column++ {
		for row := origin.Row - distance; row <= origin.Row+distance; row++ {
			coords = append(coords, C(column, row))
		}
	}
	return coords
}

// FindCoordsOnRect returns the perimeter of a rectangle. Either half-dimension
// being zero returns nothing.
func FindCoordsOnRect(origin Coord, halfWidth, halfHeight int) []Coord {
	if halfWidth == 0 || halfHeight == 0 {
		return nil
	}
	var coords []Coord
	for column := origin.Column - halfWidth; column <= origin.Column+halfWidth; column++ {
		if column == origin.Column-halfWidth || column == origin.Column+halfWidth {
			for row := origin.Row - halfHeight; row <= origin.Row+halfHeight; row++ {
				coords = append(coords, C(column, row))
			}
			continue
		}
		coords = append(coords, C(column, origin.Row-halfHeight))
		coords = append(coords, C(column, origin.Row+halfHeight))
	}
	return coords
}

// ExtrapolateAlongRay returns the coordinate reached by carrying on past
// target, along the line from origin, by offsetFromTarget cells.
//
// The line length here is the raw one, with no row doubling, unlike most of
// this file. That is upstream's choice and effects are tuned to it.
func ExtrapolateAlongRay(origin, target Coord, offsetFromTarget float64) Coord {
	base := FindLengthOfLine(origin, target, false)
	total := base + offsetFromTarget
	if total == 0 || origin == target {
		return target
	}
	t := total / base
	column := (1-t)*float64(origin.Column) + t*float64(target.Column)
	row := (1-t)*float64(origin.Row) + t*float64(target.Row)
	return C(roundHalfEven(column), roundHalfEven(row))
}
