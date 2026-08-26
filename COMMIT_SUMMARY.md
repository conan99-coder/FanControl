# FanControl — Summary

Self-hosted observability and fan-control dashboard for Vast.ai GPU rental hosts
with Gigabyte MC62-G40 motherboards (Threadripper PRO WRX80 + Aspeed AST2600
BMC running AMI MegaRAC). Built for the rig owner to monitor renters' workloads
and protect the hardware.

## What it does

- **Live dashboard** (single Go binary, embedded web app):
  - Summary strip, per-GPU cards (temp/util/power/VRAM/fan), CPU (load/temp/
    memory/per-core), Temperatures (24 BMC sensors), Drives (NVMe model/serial/
    size/temp), Disk volumes, Network throughput, Voltages
  - Light/dark theme; responsive widget grid; Edit mode to hide/rename/reorder
    rows in any widget (persisted per browser)
- **Real telemetry**:
  - Host: CPU load/temp, memory, disk + IO rates, network (`/proc`, `/sysfs`)
  - GPUs: `nvidia-smi` (no cgo, cross-compiles cleanly)
  - BMC: Redfish Thermal (temps, fan RPM) + AMI sensor API (voltages) +
    NVMe drive details from `/sys/class/nvme`
- **Fan control** (verified against this board):
  - Global mode switch **Auto / Half / Full** (the only write the BMC accepts)
  - GPU fan via `nvidia-smi` (availability probed)
  - Estimated auto duty from the active profile curve (`auto ~X%`)
  - Profiles/per-fan duty are read-only (BMC rejects PUT — verified)
- **Safety model**:
  - **Monitor mode** (default ON): server refuses all writes until admin
    switches to Control
  - **Dry-run**: collect real data, log fan actions as would-have
  - **Hard-temp governor**: auto-reverts to Auto mode on threshold breach
  - Audit log of every fan action; viewer/admin roles when auth enabled
  - Config validator refuses unauthenticated writes on non-localhost binds
    (explicit opt-out flag documented)

## Architecture

```
cmd/fanctrl/          entrypoint (providers -> poller -> control -> server)
internal/
  metrics/            telemetry model + Provider/Controller interfaces
  config/             YAML config + validation (safety rules)
  providers/
    mock/             deterministic fake for local dev
    host/             /proc + /sysfs collector
    gpu/              nvidia-smi wrapper (read + fan control)
    redfish/          BMC client (thermal, fans, FanprofileService) + AMI volts
    composite/        merges BMC + GPU controllers
  poller/             tick loop, history ring, safety governor
  control/            write service (monitor/dry-run gates, audit)
  server/             REST + SSE + embedded SPA
web/                  Vite + React SPA (embedded into the binary)
deploy/               systemd unit, example configs, install scripts
docs/                 install guide + BMC probing notes
```

## Verified behavior (live on the MC62-G40 BMC)

- Fan sensor IDs 160–190 matched the BMC web UI exactly
- SetFanMode accepts `Auto/Half/Full` (confirmed via SetFanModeActionInfo);
  PUT/OPTIONS on the Fanprofile resource return 501 — profiles are read-only
- Voltages (P_12V, P_5V, P_3V3, P_5V_STBY, P_VBAT, VR_P0_VOUT) come only from
  the AMI API; Redfish Thermal lacks them
- 24 temp sensors verified; `CPU0_DTS` excluded (non-thermal)
- Full test suite: parsers, config validation (safety refusals), redfish GBT
  schema, fan interpolation, control gates; Playwright smoke/layout/edit/
  reorder tests render the SPA headlessly

## Build & run

```bash
make                # builds SPA + cross-compiles linux/amd64 binary
                    # (or .\build.ps1 -Target linux on Windows)

./fanctrl --provider mock --bind 127.0.0.1:8080    # local demo (no hardware)
```

Install on the rig: see docs/INSTALL.md (standalone + systemd service).
README.md has the full GitHub-facing overview.

## Repo hygiene

- Secrets are handled via env vars / 0600 files; config templates use
  placeholders only
- `.gitignore` excludes binaries, web/dist, node_modules, secret-bearing
  configs, logs, and credential helpers
- History squashed to a single commit for public release (early development
  snapshots contained the BMC password/IP in now-removed history)
