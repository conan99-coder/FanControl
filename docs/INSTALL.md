# FanControl — Installation Guide

Install and run FanControl on your **Ubuntu 24.04** rig with a **Gigabyte
MC62-G40** motherboard (AMD Threadripper PRO / WRX80) and its onboard
**AMI MegaRAC SP-X** BMC (GBT `FanprofileService`). This guide covers two modes:

- **Standalone** — run it in the foreground/background for testing.
- **Systemd service** — run it managed: starts at boot, restarts on crash. **Recommended** for a 24/7 rig.

> All commands below use **placeholders** (`RIG_USER`, `RIG_HOST`, `BMC_IP`).
> Replace them with your values. Never commit passwords.

> **Hardware scope:** this guide (and FanControl's fan control) targets the
> **Gigabyte MC62-G40 + AMI MegaRAC SP-X BMC**. The sensor reading code is
> standard Linux `sysfs`/`proc`, but the **fan-mode write path and BMC schema
> are verified only on this board/firmware** — see `BMC-API-NOTES.md` before
> trusting fan control on anything else.

---

## Prerequisites

Hardware / firmware:

- Ubuntu 24.04 on the **Gigabyte MC62-G40** motherboard
- **AMI MegaRAC SP-X** BMC reachable over the management LAN (`<BMC_IP>`)
- **NVIDIA GPUs** visible to the host (`nvidia-smi` works)

Kernel drivers / modules required for reading values (no custom drivers —
FanControl uses standard interfaces):

| Value read | Kernel interface | Driver/module |
|---|---|---|
| CPU temp | `/sys/class/hwmon/*` | `k10temp` (AMD) |
| NVMe temp/model/serial/size | `/sys/class/nvme/*`, `/sys/class/block/*` | `nvme` |
| Disk usage/IO | `/proc/mounts`, statfs, `/proc/diskstats` | kernel (`md`/`dm` for RAID) |
| CPU load/mem/network | `/proc/stat`, `/proc/meminfo`, `/proc/net/dev` | kernel |
| GPU temp/util/power/VRAM/fan | `nvidia-smi` (NVML) | NVIDIA driver (proprietary or open module) |
| BMC temps/fans/modes/volts | Redfish + AMI API over the management LAN | BMC firmware **AMI MegaRAC SP-X** (no IPMI kernel module needed) |

Quick check on the rig:

```bash
grep -l . /sys/class/hwmon/hwmon*/name        # k10temp should be listed
ls /sys/class/nvme/                            # NVMe controllers visible
nvidia-smi                                     # GPUs listed
```

- A built static Linux binary (`fanctrl-linux-amd64`, ~8 MB, no dependencies).
  See the repo README for how to build it.

---


## 1. Copy the files to the rig

From your dev machine (PowerShell):

```powershell
scp deploy\fanctrl-linux-amd64 RIG_USER@RIG_HOST:/tmp/
scp deploy\fanctrl.live.yaml RIG_USER@RIG_HOST:/tmp/
scp deploy\fanctrl.service RIG_USER@RIG_HOST:/tmp/
```

> Using the `.live.yaml` (bind `127.0.0.1`) keeps the dashboard reachable only
> from the rig itself — expose it via an SSH tunnel or your own reverse proxy.
> If you want the dashboard on the open LAN, use `.lan.yaml` instead (see
> section 6).

---

## 2. Install the binary + config

On the rig:

```bash
sudo install -m 0755 /tmp/fanctrl-linux-amd64 /usr/local/bin/fanctrl
sudo mkdir -p /etc/fanctrl /var/lib/fanctrl
sudo cp /tmp/fanctrl.live.yaml /etc/fanctrl/config.yaml
```

## 3. Secrets (never in git, never in the guide)

The only secret FanControl itself needs is the **BMC password** (last 11
characters of the motherboard serial, unless you changed it). Store it in a
0600 root-only file:

```bash
sudo sh -c 'printf "%s" "YOUR_BMC_PASSWORD" > /etc/fanctrl/bmc_password'
sudo chmod 600 /etc/fanctrl/bmc_password
```

Optionally generate a session key (only used if auth is enabled):

```bash
sudo sh -c 'head -c 32 /dev/urandom | base64 > /etc/fanctrl/secret'
sudo chmod 600 /etc/fanctrl/secret
```

---

## 4. Run standalone (quick test)

```bash
sudo /usr/local/bin/fanctrl --config /etc/fanctrl/config.yaml
```

You'll see logs like `starting fanctrl ... dry_run=true` in the foreground.
Stop with Ctrl-C. To verify it serves data:

```bash
curl -s http://127.0.0.1:8080/api/health
curl -s http://127.0.0.1:8080/api/metrics | head -c 400; echo
curl -s http://127.0.0.1:8080/api/discovery | python3 -m json.tool
```

Reach the dashboard from your laptop (in another terminal):

```powershell
ssh -L 8080:127.0.0.1:8080 RIG_USER@RIG_HOST
# then open http://127.0.0.1:8080 on your laptop
```

---

## 5. Install as a systemd service (recommended for 24/7)

```bash
sudo cp /tmp/fanctrl.service /etc/systemd/system/fanctrl.service
sudo systemctl daemon-reload
sudo systemctl enable --now fanctrl
sudo systemctl status fanctrl
```

`fanctrl.service` runs the binary with `--config /etc/fanctrl/config.yaml`,
restarts on failure (`Restart=on-failure`, 5 s backoff), and starts at boot.

If the service unit content differs from the packaged one, here is the
reference unit:

```ini
[Unit]
Description=FanControl — Vast rig observability and fan control
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/fanctrl --config /etc/fanctrl/config.yaml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### Useful service commands

```bash
sudo systemctl restart fanctrl   # apply config changes
sudo systemctl stop fanctrl      # stop
sudo journalctl -u fanctrl -f    # live logs
sudo journalctl -u fanctrl --since "10 minutes ago"
```

---

## 6. Expose on the LAN (optional)

If you want the dashboard directly reachable (e.g. `http://RIG_HOST:8080`)
without a tunnel, use `deploy/fanctrl.lan.yaml` (binds `0.0.0.0:8080`) instead
of `.live.yaml`:

```bash
sudo cp /tmp/fanctrl.lan.yaml /etc/fanctrl/config.yaml
sudo systemctl restart fanctrl
```

**Security notes for the LAN config:**

- It runs with **auth disabled** by default (you said you'll gate access with a
  reverse proxy / firewall). The config file documents the one safety opt-out
  flag (`auth.allow_unauthenticated_writes`). Only expose it behind something
  that protects the port — otherwise anyone who can reach it can change fan
  modes.
- The dashboard starts in **Monitor** mode: it never writes until an admin
  clicks Control. Fan-mode switching (Auto/Half/Full) is the only write the
  board supports.

---

## 7. What FanControl can and cannot control on the MC62-G40

Verified against this board's BMC (`SetFanModeActionInfo`):

| Capability | Works? |
|---|---|
| Fan mode — `Auto` / `Half` / `Full` | ✅ the only BMC write |
| GPU fan speed (`nvidia-smi`) | ✅ if the driver allows it |
| Profile switch (default/CPU/NEW_PROFILE) | ❌ BMC rejects PUT (501) |
| Per-fan duty / curve edit | ❌ BMC rejects PUT (501) |

Fan duties are **estimated** from the active profile's curve (`arrDuty` /
`arrRef`) at the current temperatures and shown as `auto ~X%`; the BMC does not
report a live duty value.

---

## 8. Uninstall

```bash
sudo systemctl disable --now fanctrl
sudo rm -f /etc/systemd/system/fanctrl.service
sudo systemctl daemon-reload
sudo rm -f /usr/local/bin/fanctrl
sudo rm -rf /etc/fanctrl /var/lib/fanctrl
```

---

## Troubleshooting

| Symptom | Check |
|---|---|
| `provider failed ... gpu: nvidia-smi query failed` | Driver query field unsupported — the app falls back to a minimal query; confirm `nvidia-smi` works on the host |
| `provider failed ... ami` | BMC session/CSRF — confirm BMC reachable and `bmc_password` correct (AMI API provides voltages) |
| `refusing to bind ... auth disabled` | Config validator: writes + non-localhost bind requires auth or `allow_unauthenticated_writes` |
| Dashboard HTML downloads instead of rendering | Hard-refresh (Ctrl+Shift+R) to bust the cached content type |
| No voltages shown | AMI session may have expired; logs show `ami` errors — check BMC reachability |
