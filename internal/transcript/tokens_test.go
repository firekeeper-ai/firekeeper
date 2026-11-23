package transcript

import "testing"

func TestCacheTokensPrefersTheLargerFigure(t *testing.T) {
	for _, test := range []struct {
		name   string
		tokens Tokens
		want   int64
	}{
		{"combined and split agree", Tokens{Cache: 10, CacheRead: 6, CacheWrite: 4}, 10},
		{"combined only", Tokens{Cache: 10}, 10},
		{"split only", Tokens{CacheRead: 6, CacheWrite: 4}, 10},
		{"read only", Tokens{CacheRead: 6}, 6},
		{"split larger than combined", Tokens{Cache: 3, CacheRead: 6, CacheWrite: 4}, 10},
		{"combined larger than split", Tokens{Cache: 12, CacheRead: 6}, 12},
		{"none", Tokens{}, 0},
		{"negative counts", Tokens{Cache: -3, CacheRead: -1, CacheWrite: -2}, 0},
		{"negative read beside positive write", Tokens{CacheRead: -5, CacheWrite: 4}, 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := CacheTokens(test.tokens); got != test.want {
				t.Errorf("CacheTokens(%+v) = %d; want %d", test.tokens, got, test.want)
			}
		})
	}
}

func TestTotalTokens(t *testing.T) {
	for _, test := range []struct {
		name   string
		tokens Tokens
		want   int64
	}{
		{"combined cache", Tokens{Input: 100, Output: 50, Cache: 1000, CacheRead: 800, CacheWrite: 200}, 1150},
		{"split cache only", Tokens{Input: 100, Output: 50, CacheRead: 800, CacheWrite: 200}, 1150},
		{"combined cache only", Tokens{Input: 100, Output: 50, Cache: 1000}, 1150},
		{"no cache", Tokens{Input: 100, Output: 50}, 150},
		{"none", Tokens{}, 0},
		{"negative input and output", Tokens{Input: -100, Output: -50, Cache: 10}, 10},
		{"all negative", Tokens{Input: -1, Output: -1, Cache: -1, CacheRead: -1, CacheWrite: -1}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := TotalTokens(test.tokens); got != test.want {
				t.Errorf("TotalTokens(%+v) = %d; want %d", test.tokens, got, test.want)
			}
		})
	}
}

// FuzzTotalTokens checks the properties every total must keep, whatever a
// provider file holds: never negative, never below any one non-negative
// part, and the cache total never below either cache figure.
func FuzzTotalTokens(f *testing.F) {
	f.Add(int64(100), int64(50), int64(1000), int64(800), int64(200))
	f.Add(int64(-1), int64(0), int64(-5), int64(3), int64(-2))
	f.Fuzz(func(t *testing.T, input, output, cache, read, write int64) {
		// Keep sums clear of overflow; real counts are far below this.
		const limit = 1 << 60
		for _, n := range []int64{input, output, cache, read, write} {
			if n > limit || n < -limit {
				t.Skip()
			}
		}
		tokens := Tokens{Input: input, Output: output, Cache: cache, CacheRead: read, CacheWrite: write}
		c, total := CacheTokens(tokens), TotalTokens(tokens)
		if c < 0 || total < 0 {
			t.Fatalf("negative result: cache %d total %d for %+v", c, total, tokens)
		}
		if c < cache || c < max(read, 0)+max(write, 0) {
			t.Fatalf("cache %d below a cache figure of %+v", c, tokens)
		}
		if total < c || total < input || total < output {
			t.Fatalf("total %d below a part of %+v", total, tokens)
		}
	})
}
