// Command fanctrl is the FanControl observability and fan-control service.
//
// It runs a poll loop over the configured providers (host, gpu, redfish, or a
// deterministic mock), serves a dashboard over HTTP, and applies safety gates
// (dry-run and read-only) around every fan-control write.
package main

import (
	"context"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hedchr/fanctrl/internal/auth"
	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/control"
	"github.com/hedchr/fanctrl/internal/metrics"
	"github.com/hedchr/fanctrl/internal/poller"
	"github.com/hedchr/fanctrl/internal/providers/composite"
	"github.com/hedchr/fanctrl/internal/providers/gpu"
	"github.com/hedchr/fanctrl/internal/providers/host"
	"github.com/hedchr/fanctrl/internal/providers/mock"
	"github.com/hedchr/fanctrl/internal/providers/redfish"
	"github.com/hedchr/fanctrl/internal/server"
	"github.com/hedchr/fanctrl/web"
)

func main() {
	var (
		configPath = flag.String("config", "", "path to YAML config file (defaults to built-in defaults)")
		provider   = flag.String("provider", "", "force provider: real | mock (overrides config)")
		dryRun     = flag.Bool("dry-run", false, "collect real data but never write to the BMC/GPU")
		readOnly   = flag.Bool("read-only", false, "serve localhost only and disable all write endpoints")
		bind       = flag.String("bind", "", "override listen address (e.g. 127.0.0.1:8080)")
	)
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	if *provider != "" {
		cfg.Provider = *provider
	}
	if *dryRun {
		cfg.DryRun = true
	}
	if *readOnly {
		cfg.ReadOnly = true
		cfg.Listen = "127.0.0.1:" + portOf(cfg.Listen)
		cfg.Auth.Enabled = false
	}
	if *bind != "" {
		cfg.Listen = *bind
	}
	if err := cfg.Validate(); err != nil {
		log.Error("config validate", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Providers ---
	var providers []metrics.Provider
	var ctrl metrics.Controller

	switch cfg.Provider {
	case "mock":
		mp := mock.NewProvider()
		providers = append(providers, mp)
		ctrl = mock.NewController()
		log.Info("running with MOCK provider", "mode", cfg.Provider)
	default: // real
		hostP := host.NewProvider(0)
		providers = append(providers, hostP)
		var gpuP *gpu.Provider
		var gpuCtl *gpu.Controller
		if cfg.GPU.Enabled {
			gpuP = gpu.NewProvider(cfg.GPU.Query)
			providers = append(providers, gpuP)
			// Detect GPU fan-control capability (probes fan.speed readability).
			gpuCtl = gpu.NewController(cfg.GPU.Query)
		}
		var bmcCtl metrics.Controller
		if cfg.BMC.URL != "" {
			pass, err := config.ResolveSecret(cfg.BMC.PasswordPath)
			if err != nil {
				log.Warn("bmc password", "err", err)
			}
			rc := redfish.NewClient(cfg.BMC.URL, cfg.BMC.Username, pass, cfg.BMC.InsecureTLS)
			providers = append(providers, rc)
			bmcCtl = rc

			// AMI sensor readings provider: voltages (P_12V/5V/3V3/...) are only
			// available via the AMI web API, not Redfish Thermal.
			ami := redfish.NewAMIClient(cfg.BMC.URL, cfg.BMC.Username, pass, cfg.BMC.InsecureTLS)
			providers = append(providers, ami)
		}
		// Compose the controller: BMC for profiles/duty, GPU for GPU fans.
		ctrl = composite.New(bmcCtl, gpuCtl)
		if bmcCtl == nil && gpuCtl == nil {
			log.Warn("no BMC or GPU control configured; control endpoints will report unavailable")
		}
		log.Info("running with REAL providers", "host", true, "gpu", cfg.GPU.Enabled, "bmc", cfg.BMC.URL != "")
	}

	// --- History ring ---
	hist := poller.NewRing(cfg.History.Points)
	if cfg.History.Enabled && cfg.History.Path != "" {
		_ = hist.Load(cfg.History.Path)
	}

	// --- Poller ---
	p := poller.New(providers, cfg.Thresholds, hist, log)
	go p.Start(ctx, cfg.PollInterval)

	// --- Control service ---
	var audit *control.AuditLog
	audit = control.NewAuditLog(200)
	ctrlSvc := control.New(ctrl, p, control.Options{
		DryRun:   cfg.DryRun,
		ReadOnly: cfg.ReadOnly,
		Audit:    audit,
	}, log)

	// --- Auth ---
	secret, _ := config.ResolveSecret(cfg.Auth.SecretPath)
	authStore := auth.NewStore(cfg.Auth.Users, []byte(secret), cfg.Auth.SessionTTL)

	// --- Server ---
	assets := webAssets()
	srv := server.New(p, ctrlSvc, authStore, cfg.Auth.Enabled, assets, log)

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Info("starting fanctrl", "listen", cfg.Listen, "provider", cfg.Provider,
		"dry_run", cfg.DryRun, "read_only", cfg.ReadOnly, "auth", cfg.Auth.Enabled)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

// webAssets returns the embedded web dist as an fs.FS, or nil if not built.
func webAssets() fs.FS {
	sub, err := web.FS()
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

// portOf extracts the port from a listen address.
func portOf(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i+1:]
		}
	}
	return "8080"
}
