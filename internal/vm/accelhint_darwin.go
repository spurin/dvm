//go:build darwin

package vm

// accelHint returns guidance shown on TCG fallback. HVF ships with every Mac
// and rarely needs enabling, so there is no actionable hint here (a failure is
// usually a missing codesign/entitlement, addressed at build time).
func accelHint() string { return "" }
