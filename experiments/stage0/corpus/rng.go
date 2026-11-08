package corpus

// prng is SplitMix64, written out here so generated text cannot change with the
// standard library's random implementation. Its modulo reduction has a bias far
// too small to matter at these ranges.
type prng struct{ state uint64 }

const splitMixGamma = 0x9E3779B97F4A7C15

func newPRNG(seed int64, stream uint64) *prng {
	p := &prng{state: uint64(seed) ^ (stream * splitMixGamma)}
	p.next()
	return p
}

func (p *prng) next() uint64 {
	p.state += splitMixGamma
	z := p.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// intn returns a value in [0, n). n must be positive.
func (p *prng) intn(n int) int { return int(p.next() % uint64(n)) }

// between returns a value in [lo, hi].
func (p *prng) between(lo, hi int) int { return lo + p.intn(hi-lo+1) }

// chance is true with probability num/den.
func (p *prng) chance(num, den int) bool { return p.intn(den) < num }

func (p *prng) shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		swap(i, p.intn(i+1))
	}
}

func pick[T any](p *prng, xs []T) T { return xs[p.intn(len(xs))] }

func shuffled[T any](p *prng, xs []T) []T {
	p.shuffle(len(xs), func(i, j int) { xs[i], xs[j] = xs[j], xs[i] })
	return xs
}
