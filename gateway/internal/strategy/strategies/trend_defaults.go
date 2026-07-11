package strategies

// Default EMA periods for trend-following strategies.
// These are internal constants, not user-facing dynamic parameters.
// Entry/add-position indicators are configured via CRA params instead.
const (
	trendFastPeriod = 12
	trendSlowPeriod = 26
)
