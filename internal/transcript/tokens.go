package transcript

// CacheTokens is t's cache total: the combined Cache figure or the sum of
// its CacheRead and CacheWrite split, whichever is larger, since a provider
// may fill either. Readers keep Cache equal to the split's sum; taking the
// larger keeps the total right when one side is missing. Negative counts
// count as zero.
func CacheTokens(t Tokens) int64 {
	return max(nonnegative(t.Cache), nonnegative(t.CacheRead)+nonnegative(t.CacheWrite))
}

// TotalTokens is every token t counts: input, output, and CacheTokens.
// Negative counts count as zero. Every token total goes through it, so
// reports built from the same events agree.
func TotalTokens(t Tokens) int64 {
	return nonnegative(t.Input) + nonnegative(t.Output) + CacheTokens(t)
}

func nonnegative(n int64) int64 { return max(n, 0) }
