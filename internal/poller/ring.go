package poller

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Ring is a fixed-capacity rolling buffer of snapshots used for sparklines and
// the history endpoint. It keeps a bounded number of points in memory and, if
// enabled, can optionally persist to disk. It is safe for concurrent use.
type Ring struct {
	mu     sync.RWMutex
	cap    int
	points []metrics.Snapshot
	next   int
	full   bool
}

// NewRing builds a ring with the given capacity.
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = 600
	}
	return &Ring{cap: capacity, points: make([]metrics.Snapshot, capacity)}
}

// Append adds a snapshot, evicting the oldest when full.
func (r *Ring) Append(s metrics.Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.points[r.next] = s
	r.next = (r.next + 1) % r.cap
	if r.next == 0 {
		r.full = true
	}
}

// Recent returns up to n most-recent snapshots in chronological order.
func (r *Ring) Recent(n int) []metrics.Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n <= 0 || n > r.cap {
		n = r.cap
	}
	// Build in chronological order.
	var out []metrics.Snapshot
	if r.full {
		// Oldest is at r.next, iterate to r.next-1.
		for i := 0; i < r.cap; i++ {
			out = append(out, r.points[(r.next+i)%r.cap])
		}
	} else {
		for i := 0; i < r.next; i++ {
			out = append(out, r.points[i])
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// Len returns the current number of stored snapshots.
func (r *Ring) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.full {
		return r.cap
	}
	return r.next
}

// Series extracts a time-ordered slice of one scalar for charting. The extractor
// returns (value, ok). For example, GPU 0 temperature.
func (r *Ring) Series(extract func(metrics.Snapshot) (float64, bool)) []TimePoint {
	pts := r.Recent(r.cap)
	out := make([]TimePoint, 0, len(pts))
	for _, s := range pts {
		if v, ok := extract(s); ok {
			out = append(out, TimePoint{T: s.Time, V: v})
		}
	}
	return out
}

// TimePoint is one (time, value) sample for a series.
type TimePoint struct {
	T time.Time `json:"t"`
	V float64   `json:"v"`
}

// Save serializes the ring to disk (best-effort). Used to survive restarts.
func (r *Ring) Save(path string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	data, err := json.Marshal(r.Recent(r.cap))
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Load reads a previously-saved ring from disk (best-effort).
func (r *Ring) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var pts []metrics.Snapshot
	if err := json.Unmarshal(data, &pts); err != nil {
		return err
	}
	for _, s := range pts {
		r.Append(s)
	}
	return nil
}
