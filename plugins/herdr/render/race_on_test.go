//go:build race

package render

// the race detector slows the timing tests by an order of magnitude
const raceEnabled = true
