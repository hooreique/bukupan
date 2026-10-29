package main

import (
	"math"
	"sort"
)

const (
	worldLimit     = 1e9
	cellSize       = 1024.0
	maxPathSpan    = 8192.0
	maxViewSpan    = 32768.0
	maxViewStrokes = 4000
	maxViewLines   = 40000
)

type point [2]float64
type bounds [4]float64
type line [4]float64
type interval [2]float64

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func (b bounds) valid(span float64) bool {
	for _, v := range b {
		if !finite(v) || math.Abs(v) > worldLimit {
			return false
		}
	}
	return b[0] <= b[2] && b[1] <= b[3] && b[2]-b[0] <= span && b[3]-b[1] <= span
}
func (b bounds) overlaps(c bounds) bool {
	return b[0] <= c[2] && c[0] <= b[2] && b[1] <= c[3] && c[1] <= b[3]
}
func pathBounds(points []point, padding float64) bounds {
	b := bounds{points[0][0], points[0][1], points[0][0], points[0][1]}
	for _, p := range points {
		b[0] = math.Min(b[0], p[0])
		b[1] = math.Min(b[1], p[1])
		b[2] = math.Max(b[2], p[0])
		b[3] = math.Max(b[3], p[1])
	}
	return bounds{b[0] - padding, b[1] - padding, b[2] + padding, b[3] + padding}
}
func pathLines(points []point) []line {
	if len(points) == 1 {
		return []line{{points[0][0], points[0][1], points[0][0], points[0][1]}}
	}
	lines := make([]line, 0, len(points)-1)
	for i := 1; i < len(points); i++ {
		lines = append(lines, line{points[i-1][0], points[i-1][1], points[i][0], points[i][1]})
	}
	return lines
}
func mergeIntervals(cuts []interval) []interval {
	sort.Slice(cuts, func(i, j int) bool { return cuts[i][0] < cuts[j][0] })
	merged := make([]interval, 0, len(cuts))
	for _, cut := range cuts {
		cut[0], cut[1] = math.Max(0, cut[0]), math.Min(1, cut[1])
		if cut[1] < cut[0] {
			continue
		}
		if len(merged) > 0 && cut[0] <= merged[len(merged)-1][1]+1e-12 {
			merged[len(merged)-1][1] = math.Max(merged[len(merged)-1][1], cut[1])
		} else {
			merged = append(merged, cut)
		}
	}
	return merged
}
func circleCut(l line, p point, radius float64) []interval {
	dx, dy := l[2]-l[0], l[3]-l[1]
	x, y := l[0]-p[0], l[1]-p[1]
	a := dx*dx + dy*dy
	if a == 0 {
		if x*x+y*y <= radius*radius {
			return []interval{{0, 1}}
		}
		return nil
	}
	t := -(x*dx + y*dy) / a
	distance2 := (x+t*dx)*(x+t*dx) + (y+t*dy)*(y+t*dy)
	if distance2 > radius*radius {
		return nil
	}
	delta := math.Sqrt(math.Max(0, radius*radius-distance2) / a)
	return mergeIntervals([]interval{{t - delta, t + delta}})
}

// Clip the linear value start+t*delta to [low,high].
func clipInterval(t *interval, start, delta, low, high float64) bool {
	if math.Abs(delta) < 1e-12 {
		return start >= low && start <= high
	}
	a, b := (low-start)/delta, (high-start)/delta
	if a > b {
		a, b = b, a
	}
	t[0], t[1] = math.Max(t[0], a), math.Min(t[1], b)
	return t[0] <= t[1]
}

// Intersection with the swept disk, including both round end caps.
func capsuleCut(l line, eraser line, radius float64) []interval {
	cuts := append(circleCut(l, point{eraser[0], eraser[1]}, radius), circleCut(l, point{eraser[2], eraser[3]}, radius)...)
	ex, ey := eraser[2]-eraser[0], eraser[3]-eraser[1]
	length := math.Hypot(ex, ey)
	if length > 0 {
		ex, ey = ex/length, ey/length
		x, y, dx, dy := l[0]-eraser[0], l[1]-eraser[1], l[2]-l[0], l[3]-l[1]
		t := interval{0, 1}
		if clipInterval(&t, x*ex+y*ey, dx*ex+dy*ey, 0, length) && clipInterval(&t, -x*ey+y*ex, -dx*ey+dy*ex, -radius, radius) {
			cuts = append(cuts, t)
		}
	}
	return mergeIntervals(cuts)
}
func visibleLines(original line, cuts []interval) []line {
	result := []line{}
	from := 0.0
	add := func(a, b float64) {
		result = append(result, line{original[0] + (original[2]-original[0])*a, original[1] + (original[3]-original[1])*a, original[0] + (original[2]-original[0])*b, original[1] + (original[3]-original[1])*b})
	}
	for _, cut := range mergeIntervals(cuts) {
		if cut[0] > from {
			add(from, cut[0])
		}
		from = math.Max(from, cut[1])
	}
	if from < 1 {
		add(from, 1)
	}
	return result
}
