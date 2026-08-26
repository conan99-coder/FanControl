//go:build linux

package host

import "syscall"

// fsStats holds filesystem capacity in bytes.
type fsStats struct{ total, avail float64 }

func statfs(path string) (fsStats, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return fsStats{}, err
	}
	bsize := int64(st.Bsize)
	return fsStats{
		total: float64(st.Blocks) * float64(bsize),
		avail: float64(st.Bavail) * float64(bsize),
	}, nil
}

func (s fsStats) Total() float64 { return s.total }
func (s fsStats) Avail() float64 { return s.avail }
