// Package vast implements the read-only Vast.ai hosting provider.
//
// It shells out to the `vastai` CLI (`vastai show machines --raw`, the host
// command for machines you rent out) and maps the machine summary into the
// snapshot as VastRig entries: earnings ($/h and $/day), listed rate, running
// rentals, and contract end dates. The CLI is invoked at most every Interval
// (the process is too heavy to spawn every poll tick); results are cached and
// served between refreshes.
//
// Only derived, non-sensitive fields are exposed through the API — the
// machine's public IP and the API key never leave the server.
package vast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Options configures the provider.
type Options struct {
	// CLI is the command to run (default "vastai"). May be an absolute path if
	// it is not on the service's PATH (e.g. /home/<user>/.local/bin/vastai).
	CLI string
	// APIKeyPath is an optional 0600 file (or env:VAR) holding the Vast API
	// key. If set, it is passed to the CLI via VAST_API_KEY and --api-key.
	APIKeyPath string
	// Interval is the minimum time between CLI invocations (default 1m).
	Interval time.Duration
	// MarketFilter limits the GPU-market metrics to these GPU names (empty =
	// all).
	MarketFilter []string
}

// Provider implements metrics.Provider.
type Provider struct {
	opts Options

	mu         sync.Mutex
	lastFetch  time.Time
	cached     []metrics.VastRig
	cachedGpus []metrics.VastGpu
}

// NewProvider builds a Vast.ai provider.
func NewProvider(opts Options) *Provider {
	if opts.CLI == "" {
		opts.CLI = "vastai"
	}
	if opts.Interval <= 0 {
		opts.Interval = time.Minute
	}
	return &Provider{opts: opts}
}

// Name implements Provider.
func (p *Provider) Name() string { return "vast" }

// Close implements Provider.
func (p *Provider) Close() error { return nil }

// Collect implements Provider. It re-fetches at most every Interval; between
// refreshes the cached result is returned. The GPU-market metrics are
// best-effort: if that query fails, the previous metrics are kept and the rig
// data is still returned.
func (p *Provider) Collect(ctx context.Context) (metrics.Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached == nil || time.Since(p.lastFetch) >= p.opts.Interval {
		rigs, err := p.fetch(ctx)
		if err != nil {
			return metrics.Snapshot{}, err
		}
		p.cached = rigs
		if gpus, err := p.fetchMetrics(ctx); err == nil {
			p.cachedGpus = gpus
		}
		p.lastFetch = time.Now()
	}
	return metrics.Snapshot{Time: time.Now(), VastRigs: p.cached, VastGpus: p.cachedGpus}, nil
}

// fetch runs the CLI once and parses the machine list.
func (p *Provider) fetch(ctx context.Context) ([]metrics.VastRig, error) {
	out, err := p.run(ctx, "show", "machines", "--raw")
	if err != nil {
		return nil, err
	}
	return parseMachines(out)
}

// fetchMetrics queries the GPU marketplace snapshot (`vastai metrics gpu
// --verified true --raw`) and applies the configured name filter.
func (p *Provider) fetchMetrics(ctx context.Context) ([]metrics.VastGpu, error) {
	out, err := p.run(ctx, "metrics", "gpu", "--verified", "true", "--raw")
	if err != nil {
		return nil, err
	}
	gpus, err := parseGPUMetrics(out)
	if err != nil {
		return nil, err
	}
	return filterGpus(gpus, p.opts.MarketFilter), nil
}

// filterGpus keeps GPU rows whose name starts with any of the given prefixes
// (case-insensitive "begins with", so "RTX PRO 6000" matches "RTX PRO 6000 WS"
// and "RTX PRO 6000 S"). Empty filter = all rows.
func filterGpus(gpus []metrics.VastGpu, prefixes []string) []metrics.VastGpu {
	if len(prefixes) == 0 {
		return gpus
	}
	lower := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			lower = append(lower, p)
		}
	}
	if len(lower) == 0 {
		return gpus
	}
	var filtered []metrics.VastGpu
	for _, g := range gpus {
		name := strings.ToLower(g.Name)
		for _, p := range lower {
			if strings.HasPrefix(name, p) {
				filtered = append(filtered, g)
				break
			}
		}
	}
	return filtered
}

// run executes the vastai CLI once with the configured API key.
func (p *Provider) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, p.opts.CLI, args...)
	if p.opts.APIKeyPath != "" {
		key, err := readSecret(p.opts.APIKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read vast api key: %w", err)
		}
		// Both the env var and the explicit flag are used so the CLI picks the
		// key up regardless of which mechanism this version supports.
		cmd.Env = append(os.Environ(), "VAST_API_KEY="+key)
		cmd.Args = append(cmd.Args, "--api-key", key)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("vast %s: %w (%s)", p.opts.CLI, err, msg)
		}
		return nil, fmt.Errorf("vast %s: %w", p.opts.CLI, err)
	}
	return out, nil
}

// readSecret reads a 0600 file or env:VAR reference.
func readSecret(ref string) (string, error) {
	if strings.HasPrefix(ref, "env:") {
		v := os.Getenv(strings.TrimPrefix(ref, "env:"))
		if v == "" {
			return "", fmt.Errorf("environment variable %q is empty", strings.TrimPrefix(ref, "env:"))
		}
		return v, nil
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// rawMachine mirrors the fields of `vastai show machines --raw` that we use.
type rawMachine struct {
	ID             int      `json:"id"`
	Hostname       string   `json:"hostname"`
	GPUName        string   `json:"gpu_name"`
	NumGPUs        int      `json:"num_gpus"`
	ListedGPUCost  *float64 `json:"listed_gpu_cost"`
	EarnHour       float64  `json:"earn_hour"`
	EarnDay        float64  `json:"earn_day"`
	RentalsRunning int      `json:"current_rentals_running"`
	ClientEndDate  float64  `json:"client_end_date"`
	EndDate        float64  `json:"end_date"`
	Verification   string   `json:"verification"`
	Reliability    float64  `json:"reliability2"`
	Geolocation    string   `json:"geolocation"`
}

// rawGpuMetric mirrors the fields of `vastai metrics gpu --raw` that we use.
type rawGpuMetric struct {
	Name            string  `json:"gpu_name"`
	RentedVerified  int     `json:"rented_verified"`
	AvailVerified   int     `json:"avail_verified"`
	Usage           float64 `json:"usage"`
	PriceP10        float64 `json:"price_p10"`
	PriceMedian     float64 `json:"price_median"`
	PriceP90        float64 `json:"price_p90"`
	TFLOPSPerDollar float64 `json:"tflops_per_dollar"`
}

// parseGPUMetrics maps the marketplace metrics JSON to metric types.
func parseGPUMetrics(data []byte) ([]metrics.VastGpu, error) {
	var parsed struct {
		Success bool           `json:"success"`
		Gpus    []rawGpuMetric `json:"gpus"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse vast metrics gpu: %w", err)
	}
	if !parsed.Success {
		return nil, fmt.Errorf("vast metrics gpu: success=false")
	}
	gpus := make([]metrics.VastGpu, 0, len(parsed.Gpus))
	for _, g := range parsed.Gpus {
		gpus = append(gpus, metrics.VastGpu{
			Name:            g.Name,
			RentedVerified:  g.RentedVerified,
			AvailVerified:   g.AvailVerified,
			Usage:           g.Usage,
			PriceP10:        g.PriceP10,
			PriceMedian:     g.PriceMedian,
			PriceP90:        g.PriceP90,
			TFLOPSPerDollar: g.TFLOPSPerDollar,
		})
	}
	if gpus == nil {
		gpus = []metrics.VastGpu{}
	}
	return gpus, nil
}

// parseMachines maps the CLI JSON to metric types.
func parseMachines(data []byte) ([]metrics.VastRig, error) {
	var parsed struct {
		Machines []rawMachine `json:"machines"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse vast show machines: %w", err)
	}
	rigs := make([]metrics.VastRig, 0, len(parsed.Machines))
	for _, m := range parsed.Machines {
		var listed float64
		if m.ListedGPUCost != nil {
			listed = *m.ListedGPUCost
		}
		rigs = append(rigs, metrics.VastRig{
			ID:             m.ID,
			Hostname:       m.Hostname,
			GPUName:        m.GPUName,
			NumGPUs:        m.NumGPUs,
			ListedGPUCost:  listed,
			EarnHour:       m.EarnHour,
			EarnDay:        m.EarnDay,
			RentalsRunning: m.RentalsRunning,
			ClientEndDate:  m.ClientEndDate,
			EndDate:        m.EndDate,
			Verification:   m.Verification,
			Reliability:    m.Reliability,
			Geolocation:    m.Geolocation,
		})
	}
	return rigs, nil
}
