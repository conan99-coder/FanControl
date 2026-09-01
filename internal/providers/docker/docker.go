// Package docker implements the read-only Docker container provider.
//
// The renters' Vast instances run as Docker containers on the rig (named
// C.<instance_id>). This provider shells out to the docker CLI and maps each
// container's metadata — name, image/template, status, CPU%, memory — into
// the snapshot. It deliberately collects METADATA ONLY: it never execs into,
// reads files from, or fetches logs of tenant containers.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Options configures the provider.
type Options struct {
	// CLI is the docker command (default "docker").
	CLI string
	// Interval is the minimum time between CLI invocations (default 30s).
	Interval time.Duration
}

// Provider implements metrics.Provider.
type Provider struct {
	opts Options

	mu        sync.Mutex
	lastFetch time.Time
	cached    []metrics.Container
}

// NewProvider builds a Docker provider.
func NewProvider(opts Options) *Provider {
	if opts.CLI == "" {
		opts.CLI = "docker"
	}
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Second
	}
	return &Provider{opts: opts}
}

// Name implements Provider.
func (p *Provider) Name() string { return "docker" }

// Close implements Provider.
func (p *Provider) Close() error { return nil }

// Collect implements Provider. It re-fetches at most every Interval; between
// refreshes the cached result is returned.
func (p *Provider) Collect(ctx context.Context) (metrics.Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached == nil || time.Since(p.lastFetch) >= p.opts.Interval {
		containers, err := p.fetch(ctx)
		if err != nil {
			return metrics.Snapshot{}, err
		}
		p.cached = containers
		p.lastFetch = time.Now()
	}
	return metrics.Snapshot{Time: time.Now(), Containers: p.cached}, nil
}

// fetch lists containers (docker ps) and enriches them with CPU/mem (docker
// stats). Stats failures are non-fatal: containers are still reported with
// zero CPU/mem.
func (p *Provider) fetch(ctx context.Context) ([]metrics.Container, error) {
	psOut, err := p.run(ctx, "ps", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	containers, err := parsePS(psOut)
	if err != nil {
		return nil, err
	}
	statsOut, err := p.run(ctx, "stats", "--no-stream", "--format", "{{json .}}")
	if err != nil {
		// Stats are an enrichment; a failure shouldn't blank the whole list.
		return containers, nil
	}
	byName := parseStats(statsOut)
	for i := range containers {
		if s, ok := byName[containers[i].Name]; ok {
			cpu, used, total := statsMetric(s)
			containers[i].CPUsPct = cpu
			containers[i].MemUsedBytes = used
			containers[i].MemTotalBytes = total
		}
	}
	return containers, nil
}

func (p *Provider) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, p.opts.CLI, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("docker %s: %w (%s)", strings.Join(args, " "), err, msg)
		}
		return nil, fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// psLine is one `docker ps --format '{{json .}}'` record.
type psLine struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	Status string `json:"Status"`
}

// parsePS parses the newline-delimited JSON from `docker ps`.
func parsePS(data []byte) ([]metrics.Container, error) {
	var out []metrics.Container
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var l psLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			return nil, fmt.Errorf("parse docker ps line: %w", err)
		}
		out = append(out, metrics.Container{
			ID:     l.ID,
			Name:   l.Names,
			Image:  l.Image,
			Status: l.Status,
		})
	}
	if out == nil {
		out = []metrics.Container{}
	}
	return out, nil
}

// statsLine is one `docker stats --no-stream --format '{{json .}}'` record.
type statsLine struct {
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`  // e.g. "2.34%"
	MemUsage string `json:"MemUsage"` // e.g. "1.234GiB / 62.55GiB"
}

// parseStats parses the newline-delimited JSON from `docker stats`.
func parseStats(data []byte) map[string]statsLine {
	out := map[string]statsLine{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var l statsLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			continue // skip malformed records; enrichment is best-effort
		}
		out[l.Name] = l
	}
	return out
}

// statsMetric extracts CPU% and memory (used/total bytes) from one stats line.
func statsMetric(l statsLine) (cpuPct float64, used, total float64) {
	cpuPct = parsePercent(l.CPUPerc)
	parts := strings.SplitN(l.MemUsage, "/", 2)
	used = parseSize(parts[0])
	if len(parts) == 2 {
		total = parseSize(parts[1])
	}
	return cpuPct, used, total
}

func parsePercent(s string) float64 {
	var v float64
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &v); err != nil {
		return 0
	}
	return v
}

// parseSize parses docker size strings like "1.234GiB", "512MiB", "12GB".
func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	var num float64
	var unit string
	if n, err := fmt.Sscanf(s, "%f%s", &num, &unit); err != nil || n < 1 {
		return 0
	}
	mult := map[string]float64{
		"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
		"KIB": 1 << 10, "MIB": 1 << 20, "GIB": 1 << 30, "TIB": 1 << 40,
	}
	return num * mult[strings.ToUpper(unit)]
}
