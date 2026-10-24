package sim

import (
	"hash/fnv"
	"math"
	"time"
)

// Every random decision is a pure function of the seed and stable identifiers
// (a receipt, an entity id, an operation name). It never reads a shared random
// sequence, so concurrent scheduling cannot change which fault hits which
// checkout.

// hashParts hashes the seed and the parts into one number, so the same inputs always give the same random choice.
func hashParts(seed uint64, parts ...string) uint64 {
	h := fnv.New64a()
	var s [8]byte
	for i := range s {
		s[i] = byte(seed >> (8 * i))
	}
	_, _ = h.Write(s[:])
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return mix64(h.Sum64())
}

// mix64 is the splitmix64 finalizer; FNV alone has weak low bits.
func mix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// unit returns a reproducible value in [0, 1).
func unit(seed uint64, parts ...string) float64 {
	return float64(hashParts(seed, parts...)>>11) / (1 << 53)
}

// lognormal returns a reproducible duration with the given median. sigma is the
// spread of the underlying normal distribution (0 = constant). The shape is an
// assumption: Razorpay's real latency distribution is not documented.
func lognormal(median time.Duration, sigma float64, seed uint64, parts ...string) time.Duration {
	if median <= 0 {
		return 0
	}
	u1 := unit(seed, append(parts, "u1")...)
	u2 := unit(seed, append(parts, "u2")...)
	if u1 < 1e-12 {
		u1 = 1e-12
	}
	z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	return time.Duration(float64(median) * math.Exp(sigma*z))
}

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// entityID builds a Razorpay-style id ("order_" + 14 base62 characters) that is
// stable for the same seed and parts.
func entityID(prefix string, seed uint64, parts ...string) string {
	h := hashParts(seed, parts...)
	out := make([]byte, 14)
	for i := range out {
		if i == 7 {
			h = mix64(h)
		}
		out[i] = base62[h%62]
		h /= 62
	}
	return prefix + string(out)
}
