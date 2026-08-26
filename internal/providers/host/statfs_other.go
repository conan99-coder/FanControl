//go:build !linux

package host

import "errors"

// fsStats holds filesystem capacity in bytes.
type fsStats struct{ total, avail float64 }

// statfs is a stub for non-Linux platforms. The host provider is only invoked on
// the Linux rig; on Windows it simply reads no mounts (its /proc paths don't
// exist), so an error here is harmless. This keeps the package cross-compiling.
func statfs(string) (fsStats, error) {
	return fsStats{}, errors.New("statfs not supported on this platform")
}

func (s fsStats) Total() float64 { return s.total }
func (s fsStats) Avail() float64 { return s.avail }
