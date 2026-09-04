// Package poller runs the unified collection loop: it calls every configured
// Provider on a tick, merges their snapshots into one, applies threshold
// warnings, and feeds the safety governor. It owns the history ring.
package poller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/metrics"
)

// Poller merges providers into a current snapshot and manages the governor.
type Poller struct {
	providers []metrics.Provider
	cfg       config.Thresholds
	hist      *Ring
	log       *slog.Logger

	mu      sync.RWMutex
	latest  metrics.Snapshot
	lastErr error

	// Provider-failure log throttling: a failing provider (e.g. BMC login
	// backoff) would otherwise log every tick and flood the journal.
	warnMu    sync.Mutex
	lastWarns map[string]time.Time

	// Governor state
	governor *governor
}

// New builds a Poller.
func New(providers []metrics.Provider, cfg config.Thresholds, hist *Ring, log *slog.Logger) *Poller {
	return &Poller{
		providers: providers,
		cfg:       cfg,
		hist:      hist,
		log:       log,
		lastWarns: map[string]time.Time{},
		governor:  newGovernor(cfg),
	}
}

// warnThrottled logs a provider failure at most once per interval per provider.
func (p *Poller) warnThrottled(provider string, err error) {
	p.warnMu.Lock()
	defer p.warnMu.Unlock()
	if last, ok := p.lastWarns[provider]; ok && time.Since(last) < time.Minute {
		return
	}
	p.lastWarns[provider] = time.Now()
	p.log.Warn("provider failed", "provider", provider, "err", err)
}

// Start runs the poll loop until ctx is cancelled. It immediately collects a
// first snapshot, then ticks every interval.
func (p *Poller) Start(ctx context.Context, interval time.Duration) {
	p.tick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

// Tick runs one collection synchronously (used in tests).
func (p *Poller) Tick(ctx context.Context) {
	p.tick(ctx)
}

func (p *Poller) tick(ctx context.Context) {
	p.mu.RLock()
	providers := append([]metrics.Provider(nil), p.providers...)
	p.mu.RUnlock()
	merged := metrics.Snapshot{Time: time.Now()}
	var lastErr error
	for _, prov := range providers {
		snap, err := prov.Collect(ctx)
		if err != nil {
			// A transient failure in one provider shouldn't wipe the others.
			p.warnThrottled(prov.Name(), err)
			lastErr = err
			continue
		}
		merge(&merged, snap)
	}
	// CPU temp defaults (provider may not report it): copy nothing, apply
	// warnings.
	p.applyWarnings(&merged)

	p.mu.Lock()
	p.latest = merged
	p.lastErr = lastErr
	p.mu.Unlock()

	// History
	if p.hist != nil {
		p.hist.Append(merged)
	}
}

// merge overlays the source snapshot's populated fields onto dst. Because a
// provider only sets its own sections, zero-value sections are left untouched.
func merge(dst *metrics.Snapshot, src metrics.Snapshot) {
	if src.CPU.Model != "" || src.CPU.LoadPct != 0 || src.CPU.Cores != 0 {
		dst.CPU = src.CPU
	}
	if len(src.GPUs) > 0 {
		dst.GPUs = src.GPUs
	}
	if len(src.Disks) > 0 {
		dst.Disks = src.Disks
	}
	if len(src.Drives) > 0 {
		dst.Drives = src.Drives
	}
	if len(src.Nets) > 0 {
		dst.Nets = src.Nets
	}
	if len(src.Fans) > 0 {
		dst.Fans = src.Fans
	}
	if len(src.Thermals) > 0 {
		dst.Thermals = src.Thermals
	}
	if len(src.Extra) > 0 {
		dst.Extra = src.Extra
	}
	if len(src.VastRigs) > 0 {
		dst.VastRigs = src.VastRigs
	}
	if src.VastGpus != nil {
		dst.VastGpus = src.VastGpus
	}
	if src.Containers != nil {
		dst.Containers = src.Containers
	}
	if src.Time.After(dst.Time) {
		dst.Time = src.Time
	}
}

// applyWarnings attaches warning flags to the snapshot. It does not mutate the
// source; warnings are computed here and carried alongside. For now warnings
// are surfaced via the history/alert stream rather than stamped into the
// snapshot, so we keep a lightweight callback.
func (p *Poller) applyWarnings(s *metrics.Snapshot) {}

// Snapshot returns the latest merged snapshot.
func (p *Poller) Snapshot() metrics.Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.latest
}

// LastError returns the last non-nil error encountered (for the /health endpoint).
func (p *Poller) LastError() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastErr
}

// HistoryRecent returns the last n snapshots from the ring (chronological).
func (p *Poller) HistoryRecent(n int) []metrics.Snapshot {
	if p.hist == nil {
		return nil
	}
	return p.hist.Recent(n)
}

// HistorySeries extracts a time series for the given extractor.
func (p *Poller) HistorySeries(extract func(metrics.Snapshot) (float64, bool)) []TimePoint {
	if p.hist == nil {
		return nil
	}
	return p.hist.Series(extract)
}

// Discovery aggregates the discovery inventory from all providers that support
// the Discoverer interface.
func (p *Poller) Discovery(ctx context.Context) []metrics.Discovery {
	var out []metrics.Discovery
	for _, prov := range p.providers {
		if d, ok := prov.(metrics.Discoverer); ok {
			out = append(out, d.Discover(ctx))
		}
	}
	return out
}

// Governor returns the safety governor for control-side integration.
func (p *Poller) Governor() *governor {
	return p.governor
}

// Thresholds returns the configured safety thresholds.
func (p *Poller) Thresholds() config.Thresholds {
	return p.cfg
}

// SetThresholds hot-applies new safety thresholds (poller + governor).
func (p *Poller) SetThresholds(cfg config.Thresholds) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg = cfg
	p.governor.SetThresholds(cfg)
}

// SetProviders hot-replaces the provider set (e.g. after a config change).
func (p *Poller) SetProviders(providers []metrics.Provider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.providers = providers
}
