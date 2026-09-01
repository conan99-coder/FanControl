package control

import (
	"errors"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/metrics"
)

// errReadOnly is returned when a write is attempted in read-only mode.
var errReadOnly = errors.New("write denied: service is read-only")

// errMonitor is returned when a write is attempted in Monitor mode (the app
// reports values only and must never change anything).
var errMonitor = errors.New("write denied: monitor mode — display only")

// errGPUFanLocked is returned when GPU fan control is unsupported.
var errGPUFanLocked = errors.New("GPU fan control not available on this card")

// Status is the dynamic state reported to the UI.
type Status struct {
	ReadOnly        bool                 `json:"read_only"`
	DryRun          bool                 `json:"dry_run"`
	Monitor         bool                 `json:"monitor"`
	GovernorTripped bool                 `json:"governor_tripped"`
	GovernorReason  string               `json:"governor_reason,omitempty"`
	GovernorTime    time.Time            `json:"governor_time,omitempty"`
	Capabilities    metrics.Capabilities `json:"capabilities"`
	// Thresholds exposes the configured safety limits so the UI can draw
	// warn/hard lines on charts.
	Thresholds config.Thresholds `json:"thresholds"`
}

// AuditEntry is a single fan-control action record.
type AuditEntry struct {
	Actor  string         `json:"actor"`
	Action string         `json:"action"`
	Detail map[string]any `json:"detail,omitempty"`
	Result string         `json:"result"`
	Time   time.Time      `json:"time"`
}

// AuditLog is a bounded, in-memory audit trail of fan actions.
type AuditLog struct {
	mu      sync.Mutex
	entries []AuditEntry
	max     int
}

// NewAuditLog builds an audit log holding up to max entries.
func NewAuditLog(max int) *AuditLog {
	if max <= 0 {
		max = 200
	}
	return &AuditLog{max: max}
}

// Add records an entry.
func (a *AuditLog) Add(e AuditEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	a.entries = append(a.entries, e)
	if len(a.entries) > a.max {
		a.entries = a.entries[len(a.entries)-a.max:]
	}
}

// Recent returns the most recent entries in chronological order.
func (a *AuditLog) Recent() []AuditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]AuditEntry, len(a.entries))
	copy(out, a.entries)
	return out
}
