package craft

import (
	"cmp"
	"math"
	"slices"
	"strconv"
)

// oklab is a colour in the OKLab space, where the distance between two points
// is close to how different the colours look.
type oklab struct{ l, a, b float64 }

// parseColor reads a #RRGGBB value.
func parseColor(hex string) (oklab, bool) {
	if !hexColor.MatchString(hex) {
		return oklab{}, false
	}
	value, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return oklab{}, false
	}
	linear := func(channel uint64) float64 {
		c := float64(channel&0xFF) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	r, g, b := linear(value>>16), linear(value>>8), linear(value)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return oklab{
		l: 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		a: 1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		b: 0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}, true
}

func (c oklab) distance(other oklab) float64 {
	dl, da, db := c.l-other.l, c.a-other.a, c.b-other.b
	return math.Sqrt(dl*dl + da*da + db*db)
}

// byColor keeps the values that have a colour and orders them from the closest
// to target; equally close ones keep their order.
func byColor[T any](values []T, target oklab, hex func(T) string) []T {
	type scored struct {
		value    T
		distance float64
	}
	found := make([]scored, 0, len(values))
	for _, value := range values {
		if color, ok := parseColor(hex(value)); ok {
			found = append(found, scored{value: value, distance: target.distance(color)})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int { return cmp.Compare(a.distance, b.distance) })
	out := make([]T, len(found))
	for i := range found {
		out[i] = found[i].value
	}
	return out
}

// window returns the page of values that limit and offset ask for.
func window[T any](values []T, limit, offset int32) []T {
	start := min(int(offset), len(values))
	return values[start:min(start+int(limit), len(values))]
}
