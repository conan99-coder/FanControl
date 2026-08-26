# FanControl

A self-hosted **GPU-rig observability and fan-control dashboard** for Vast.ai
rental hosts with **Gigabyte MC62-G40** motherboards (Threadripper PRO WRX80 +
Aspeed AST2600 BMC running **AMI MegaRAC SP-X** with the GBT FanprofileService).

> **Supported hardware:** Gigabyte **MC62-G40** motherboard + **AMI MegaRAC
> SP-X** BMC (GBT `FanprofileService`) + NVIDIA GPUs. The fan-control
> integration is schema-specific to this board/firmware — see
> [Hardware compatibility](#hardware-compatibility) and
> [Motherboard drivers & interfaces](#motherboard-drivers--interfaces-used-to-read-values).

FanControl runs as a small Go service on the rig, collects live telemetry from
the host, the NVIDIA GPUs, and the BMC, and serves a responsive web dashboard.
It's built for **host owners who rent out their GPUs**: watch what renters'
workloads are doing to your temps, fans, drives, and network — and control the
fans safely, with a hard-temperature safety governor.

## Why it exists

Vast.ai rental hosts run heavy, uncontrolled GPU workloads 24/7. This project
started because the BMC web UI is clunky and there was no clean way to see
"what's my hardware doing right now, and is it being protected." FanControl
gives you one dashboard for all of it.

## Features

- **Live dashboard** (single-page app, served by the Go binary, zero external
  frontend services):
  - Summary strip, per-GPU cards (temp/util/power/VRAM/fan), CPU (load/temp/
    memory/per-core), Temperatures, **Drives (NVMe: model/serial/size/temp)**,
    Disk volumes, Network throughput, Voltages
  - **Light/dark theme** toggle; responsive grid (reflows on narrow windows)
- **Real hardware telemetry**:
  - Host: CPU load/temp, memory, disk usage + IO rates, network (from `/proc`,
    `/sysfs`)
  - GPUs: `nvidia-smi` (temp, util, power, VRAM, fan) — no cgo, cross-compiles
  - BMC: Redfish Thermal (24+ temp sensors, 10 fans) + **AMI sensor API for
    voltages** (`P_12V`, `P_5V`, `P_3V3`, …)
  - NVMe drives: model, serial, size, temperature from `/sys/class/nvme`
- **Fan control** (verified against the MC62-G40 BMC):
  - Global mode switch: **Auto / Half / Full** (the only write the BMC accepts)
  - GPU fan control via `nvidia-smi` (availability probed)
  - Estimated auto duty from the active profile curve (`auto ~X%`); profiles
    themselves are read-only on this board (BMC rejects PUT)
- **Safety first**:
  - **Monitor mode** (default ON): the dashboard displays values and refuses
    every write — server-enforced, not just UI
  - **Dry-run**: collect real data, log every fan action as "would have done"
  - **Hard-temp governor**: auto-reverts to Auto fan mode if a temp exceeds the
    hard limit
  - **Audit log** of every fan action; viewer/admin roles if auth is enabled
- **Configurable dashboard**: Edit mode — hide/rename/reorder rows in every
  widget (fans, temps, drives, disk, network, volts); persists to localStorage

## Hardware compatibility

**FanControl is built FOR — and tested against — a specific motherboard and
BMC.** It is a *general* host monitor, but the **fan-control integration
targets this exact platform**. Running it on other boards may show data
incorrectly or not at all.

| Component | Target / tested platform |
|---|---|
| **Motherboard** | **Gigabyte MC62-G40** (WRX80 chipset, AMD Threadripper PRO) |
| **BMC** | Onboard **Aspeed AST2600**, **AMI MegaRAC SP-X** firmware, exposing the **GBT `FanprofileService`** (Redfish) |
| **BMC interface** | `https://<BMC_IP>/redfish/v1/...` via the management LAN port (`USB3_MLAN1`) |
| GPUs | NVIDIA (supported by `nvidia-smi`; developed against RTX 6000 Pro Blackwell WS) |
| Host OS | Ubuntu 24.04 (tested) — any modern Linux with NVIDIA driver |

The BMC integration is schema-specific: the GBT `Fanprofile` resource
(`arrDuty`/`arrRef`/`arrPolicy`, `strMode`) and the `SetFanMode` action
accepting `Auto/Half/Full`. Other Gigabyte/AMI boards are similar but **not
guaranteed** — see `BMC-API-NOTES.md` for the probing notes that established
this, and how to verify a different board.

## Motherboard drivers & interfaces used to read values

FanControl reads values from **three different layers**. There are **no
custom drivers** — it uses the standard Linux kernel interfaces and the BMC's
own firmware APIs:

### 1. Linux kernel drivers / interfaces (on the rig, host-side)

| Value | Kernel interface | Driver/module involved |
|---|---|---|
| CPU temperature | `/sys/class/hwmon/*/temp*_input` | **`k10temp`** (AMD, matches Threadripper PRO) |
| CPU model / core count | `/proc/cpuinfo` | — (kernel) |
| CPU load / memory | `/proc/stat`, `/proc/meminfo` | — (kernel) |
| Drive (NVMe) temp | `/sys/class/nvme/nvme*/hwmon*/temp*_input` | **`nvme`** driver (hwmon) |
| NVMe model / serial / size | `/sys/class/nvme/nvme*/model`, `serial`, `/sys/class/block/*/size` | **`nvme`** driver |
| Disk usage + IO rates | `/proc/mounts`, statfs, `/proc/diskstats` | — (kernel, `md`/`dm`/`raid` drivers for RAID volumes) |
| Network throughput | `/proc/net/dev` | — (kernel, any NIC driver; works for `docker0`/`veth` too) |

### 2. NVIDIA driver (GPUs)

| Value | Interface | Driver |
|---|---|---|
| GPU temp/util/power/VRAM/fan | `nvidia-smi` (NVML) | **NVIDIA proprietary or open kernel module** (`nvidia-driver-*`, e.g. 580.x) |
| GPU fan control | `nvidia-smi ... -c` | NVIDIA driver (only if the card allows fan writes) |

### 3. BMC firmware (no host driver — accessed over the network)

| Value | Interface | Firmware API |
|---|---|---|
| Motherboard temps (24 sensors) | `GET /redfish/v1/Chassis/1/Thermal` | **AMI MegaRAC SP-X** Redfish |
| Fan RPMs + tach IDs | Redfish Thermal (`SensorNumber`) | **AMI MegaRAC SP-X** |
| Fan modes `Auto/Half/Full` | `POST .../FanMode/Actions/FanMode.SetFanMode` | **AMI MegaRAC SP-X** |
| Voltages (P_12V/5V/3V3/…) | `GET /api/detail_sensors_readings` (session+CSRF) | **AMI MegaRAC SP-X** web API |

> The BMC is accessed over the **management LAN** — no IPMI kernel driver
> (`ipmi_si`/`ipmi_devintf`) is required. If you want IPMI access alongside,
> that's a separate kernel module, not used by FanControl.

**Why this matters:** if you run this on a different Gigabyte/AMI board, the
k10temp and nvme paths are standard and should work, but the **BMC schema
(GBT Fanprofile) is board/firmware specific** — the fan-mode action and
profile structure were verified only on the MC62-G40's firmware. See
`BMC-API-NOTES.md` for how to re-probe a different board before trusting fan
control.

## Getting started

### Prerequisites

- Go 1.26+
- Node 20+ (only to build the embedded web app)
- (Optional) a Linux rig with NVIDIA driver + network to the BMC

### Download & build

```bash
git clone https://github.com/conan99-coder/FanControl.git
cd FanControl
make             # builds web/ then cross-compiles a static linux/amd64 binary
```

Or on Windows (PowerShell):

```powershell
.\build.ps1 -Target linux
```

Output: `deploy/fanctrl-linux-amd64` — a single static binary (~8 MB) that
serves the dashboard and talks to the BMC. No Docker, no runtime deps on the
rig.

### Run locally with mock data (no hardware)

```bash
./fanctrl --provider mock --bind 127.0.0.1:8080
# open http://127.0.0.1:8080
```

The mock provider drives the whole app with deterministic fake data so you can
try the dashboard, edit mode, and fan controls on any machine.

### Install on the rig

See **[docs/INSTALL.md](docs/INSTALL.md)** — standalone run and systemd service
setup, with all commands and safety notes.

## Dashboard in action

Once running (systemd or standalone), open the dashboard:

- **Monitor mode** (default): everything read-only
- Click **⚙️ Control** to allow fan-mode writes (still dry-run until you flip
  the config flag)
- **✏️ Edit** to hide/rename/reorder rows in the widgets

## API

| Method + path | Auth | Description |
|---|---|---|
| `GET /api/metrics` | viewer | Full live snapshot (CPU, GPUs, drives, disks, nets, fans, thermals, extra) |
| `GET /api/discovery` | viewer | Detected hardware inventory (sensor IDs, GPU indices, drive serials, voltages) |
| `GET /api/status` | viewer | Modes (monitor/dry-run), governor state, capabilities |
| `GET /api/stream` | viewer | SSE live updates |
| `GET /api/fan/profiles` | admin | BMC fan profiles (read-only curves) |
| `GET /api/fan/active` | admin | Active mode + profile |
| `POST /api/fan/mode` | admin | Switch fan mode: `{"mode":"Auto\|Half\|Full"}` |
| `POST /api/fan/gpu` | admin | Set GPU fan: `{"gpu":0,"pct":50}` |
| `POST /api/mode` | admin | Toggle Monitor/Control: `{"monitor":true\|false}` |
| `GET /api/audit` | admin | Audit trail of fan actions |

## Config

Configuration is YAML (`deploy/fanctrl.example.yaml` documents every field).
Key knobs:

- `provider`: `real` (rig) or `mock` (local dev)
- `dry_run`: if true, never write to the BMC/GPU (default **true**)
- `auth.enabled` + `users`: optional login with roles (viewer/admin)
- `bmc.url` / `password_path`: BMC address + 0600 secret file
- `thresholds`: warn/hard temp gates for the governor
- `layout`: default widget grid

Secrets (BMC password, session key) live in **0600 files** referenced by the
config — never in the repo or on the command line.

## Safety model (read me)

1. **Monitor mode is the default state** — the server refuses every write
   until an admin switches to Control. This holds even with `dry_run: false`.
2. **Dry-run** makes writes log-only, so you can validate readings before
   trusting control.
3. **The governor** watches temps and reverts to Auto fan mode on a hard
   breach (thresholds in config).
4. **Audit**: every fan action is recorded with actor, action, and result.
5. **Capabilities are honest**: if the board can't do something (profile
   switch, per-fan duty), the app says so instead of pretending.

## Testing

```bash
make check       # go vet + go test ./...
```

Tests cover the parsers, config validation (including safety refusals), the
redfish GBT schema, fan interpolation, and control gates. A Playwright smoke
test (`web/smoke.mjs`) renders the dashboard against the mock provider.

## Project layout

```
cmd/fanctrl/          entrypoint (wires providers -> poller -> control -> server)
internal/
  metrics/            telemetry model + Provider/Controller interfaces
  config/             YAML config validation (safety rules)
  providers/
    mock/             deterministic fake for local dev
    host/             /proc + /sysfs collector
    gpu/              nvidia-smi wrapper (read + fan control)
    redfish/          BMC client (thermal, fans, FanprofileService) + AMI voltages
    composite/        merges BMC + GPU controllers
  poller/             tick loop, history ring, safety governor
  control/            write service with monitor/dry-run gates + audit
  server/             REST + SSE + embedded SPA
web/                  Vite + React SPA (embedded into the binary)
deploy/               systemd unit, example configs, install docs
docs/                 install guide + BMC notes
```

## License

MIT — see the LICENSE file.

## Roadmap / ideas

- Curve editor (if a BMC variant supports profile writes)
- Persistent server-side widget layout
- Alert notifications (webhook/ntfy) on threshold breaches
