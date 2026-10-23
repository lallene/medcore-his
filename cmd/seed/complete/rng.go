package complete

import (
	"hash/fnv"
	"math/rand/v2"
	"time"
)

// RNG is a deterministic pseudo-random source. Independent streams are derived from the
// dataset seed and a stable name, so adding draws in one domain never shifts another one.
type RNG struct{ r *rand.Rand }

func newRNG(seed uint64, stream string, parts ...uint64) *RNG {
	h := fnv.New64a()
	_, _ = h.Write([]byte(stream))
	k := h.Sum64()
	for _, p := range parts {
		k = k*1099511628211 ^ p
	}
	return &RNG{r: rand.New(rand.NewPCG(seed, k))}
}

// Stream derives an independent child stream.
func (g *RNG) Stream(name string, parts ...uint64) *RNG {
	return newRNG(g.r.Uint64(), name, parts...)
}

func (g *RNG) Intn(n int) int {
	if n <= 1 {
		return 0
	}
	return g.r.IntN(n)
}

// Between returns an int in [lo, hi].
func (g *RNG) Between(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + g.r.IntN(hi-lo+1)
}

func (g *RNG) Float() float64 { return g.r.Float64() }

// Chance is true with probability p.
func (g *RNG) Chance(p float64) bool { return g.r.Float64() < p }

// Pick returns one element.
func Pick[T any](g *RNG, items []T) T { return items[g.Intn(len(items))] }

// Weighted picks an index according to weights (all >= 0, at least one > 0).
func (g *RNG) Weighted(weights []float64) int {
	total := 0.0
	for _, w := range weights {
		total += w
	}
	x := g.r.Float64() * total
	for i, w := range weights {
		if x < w {
			return i
		}
		x -= w
	}
	return len(weights) - 1
}

// Minutes returns a duration of [lo, hi] minutes.
func (g *RNG) Minutes(lo, hi int) time.Duration {
	return time.Duration(g.Between(lo, hi)) * time.Minute
}

// Shuffle shuffles in place.
func Shuffle[T any](g *RNG, items []T) {
	g.r.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
}
