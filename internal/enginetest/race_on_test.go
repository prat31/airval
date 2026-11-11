//go:build race

package enginetest

// The race detector slows SQLite ~10×; timing assertions are skipped under -race.
const raceEnabled = true
