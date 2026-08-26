# Gigabyte MC62-G40 BMC — Login & API Notes

Findings from setting up fan monitoring / control on the vLLM server
(host accessible at the GPU server). Captured 2026-08-16, revised 2026-08-26
with live verification results. All addresses/credentials are placeholders —
replace with your own values; never commit secrets.

---

## 1. Hardware / access

| Item | Value |
|---|---|
| Motherboard | Gigabyte **MC62-G40** (Rev 1.x, AMD Threadripper PRO / WRX80) |
| BMC chip | Aspeed **AST2600** |
| BMC firmware | Gigabyte Management Console (**AMI MegaRAC SP-X**) |
| BMC address | `https://<BMC_IP>` (management LAN port "USB3_MLAN1") |
| Login user | `admin` |
| Login password | BMC web UI default = **last 11 chars of the motherboard serial number**. Older firmware uses `password`. |

> Keep the password out of source control (use an env var / a local 0600 file).
> FanControl stores it at `/etc/fanctrl/bmc_password` (0600).

---

## 2. Authentication — two separate models

The BMC exposes **two** REST surfaces with different auth:

### A. AMI web API (used by the web UI, needs session + CSRF)
Base: `https://<BMC_IP>`
- Login is NOT HTTP Basic. Flow:
  1. `GET /` (any page) → server sets a first **anonymous `QSESSIONID`** cookie (must be present before login).
  2. `POST /api/session` with **form-urlencoded** `username` / `password`, plus the seeded cookie, `X-CSRFTOKEN: null`, `Origin` header.
  3. Response sets **`QSESSIONID`** cookie (secure, HttpOnly) and returns JSON `{ "ok": 0, ..., "CSRFToken": "…", "racsession_id": N }`.
- Every **subsequent** call must send:
  - Cookie: `QSESSIONID=…; __Host-garc=<token>`
  - Header: `X-CSRFTOKEN: <token>`
  - The `__Host-garc` cookie value **must equal** the CSRF token or the call returns **401** (and, on bad paths, `{"error":"Invalid API Call","code":1010}`).

Working curl (full flow):

```bash
rm -f /tmp/bmcjar
curl -k -s -c /tmp/bmcjar https://<BMC_IP>/ -o /dev/null
curl -k -s -b /tmp/bmcjar -c /tmp/bmcjar -X POST https://<BMC_IP>/api/session \
  -H 'Content-Type: application/x-www-form-urlencoded; charset=UTF-8' \
  --data 'username=admin&password=<BMC_PASSWORD>'
```

### B. Redfish API (simpler — HTTP Basic auth, NO CSRF)
Base: `https://<BMC_IP>/redfish/v1`
- Authenticate with `-u admin:<BMC_PASSWORD>` (basic auth). Much easier for a monitoring site / scripts.

```bash
curl -k -s -u admin:<BMC_PASSWORD> https://<BMC_IP>/redfish/v1/
```

---

## 3. Which endpoints exist (tested)

| Endpoint | Auth | Result |
|---|---|---|
| `POST /api/session` | form + seeded cookie | **200 OK** — login works |
| `GET /api/detail_sensors_readings` | session + CSRF | works — fan RPM, temps, volts |
| `GET /api/settings/fan/mode` | session + CSRF | **404 `Invalid API Call`** (NOT on this board) |
| `GET /api/settings/fan/names` | any | **404 `Invalid API Call`** (NOT on this board) |
| `GET /redfish/v1/Chassis/1/Thermal/FanprofileService` | basic | **200** — fan profile service exists |
| `GET /redfish/v1/Chassis/1/Thermal` | basic | thermal + fan sensors (Redfish standard) |

> The AMI **fan** endpoints (`/api/settings/fan/*`) that fanpilot targets for ASRock Rack **do not exist** on this Gigabyte BMC. Use **Redfish FanprofileService** for fans instead.

---

## 4. Fan control — Redfish `FanprofileService` (verified)

Service root:
```
/redfish/v1/Chassis/Self/Thermal/FanprofileService
```
Children (note the `/Chassis/Self/` prefix on child paths):
- `Fanprofile` — the fan policy profiles (**GET works; PUT is rejected — see below**)
- `FanMode` — current mode + `SetFanMode` action
- `SupportPCIEDevice`

### Profile structure (GET Fanprofile / FanprofileService/Fanprofile)
Board ships profiles with `strName`: `default`, `CPU`, `NEW_PROFILE`. Active profile (`strMode`) was `CPU`.
Each profile (`arrProfile`) contains policies (`arrPolicy`), each with:

```
arrDuty        [duty% per ref point]              e.g. [30, 85, 100]
arrFanSensor   fan sensor IDs controlled          e.g. [160,161,162,184,186,187,188,189,190]
arrRef         temperature ref points (°C)        e.g. [45, 70, 85]
arrSensor      temperature sensor IDs             e.g. [1]
iInitDuty      initial duty at boot               e.g. 30
iPolicyType    1=..., 2=...
iInSDR         1
```

- Fan sensor IDs seen: `160,161,162,184,185,186,187,188,189,190` — CPU + chassis fan tach sensors (verified against the BMC web UI "Fan speed" table).
- Additional GBT-only fields ignored: `iAmbientSensor`, `iCpuTdp`, `iHysteresis`, `iPCIEDeviceEnable`, `iSensorCode`, `arrHexDeviceID/VendorID`.

### FanMode + action (VERIFIED caveats)
```
GET .../FanprofileService/FanMode
```
returns `FanMode` (e.g. `"nil"` when Auto) + an action target:
```
/redfish/v1/Chassis/Self/Thermal/FanprofileService/FanMode/Actions/FanMode.SetFanMode
```

**Verified on 2026-08-26:**

1. `SetFanMode` accepts `FanMode: "Auto" | "Half" | "Full"` (from `SetFanModeActionInfo` AllowableValues). It is a **global** mode switch — there is **no per-fan manual/auto split** and **no profile-name parameter**.
2. `PUT` (and `OPTIONS`) on the `Fanprofile` resource return **501 — "does not support the PUT method for any resource"**. So:
   - **Profile switching (default/CPU/NEW_PROFILE) is NOT possible over Redfish** — the profiles are read-only curve definitions.
   - **Per-fan duty / curve edits are NOT possible** — any write that PUTs the Fanprofile is rejected.
3. The only BMC fan write is the mode switch (Auto/Half/Full). Fan duty %s shown by the BMC web UI are computed internally; FanControl **estimates** them from the active profile's curve at the current temperatures.

---

## 5. Monitoring (for the future web site)

- **Redfish standard thermal sensors** (easy, no CSRF):
  `GET /redfish/v1/Chassis/1/Thermal` → temperatures + fans with thresholds.
- **AMI web sensor readings** (faster, needs session+CSRF):
  `GET /api/detail_sensors_readings` → fan RPM, temps, volts.
- **GPU (optional, from the host side, not the BMC):** NVML via `nvidia-ml-py`
  (`nvidia-smi` on the server) for per-GPU temp/power/fan.

Recommended approach for a monitoring panel: use **Redfish** (basic auth, documented)
for chassis temp/fans, plus NVML from the server for GPU data; use the AMI
`/api/detail_sensors_readings` only if you need the faster per-sensor readings
or the **voltages** (P_12V/5V/3V3 — not exposed by Redfish Thermal).

---

## 6. Gotchas (avoid time-wasters)

1. Login needs the **seeded anonymous QSESSIONID cookie first** — a bare
   `POST /api/session` with no cookie returns **401**.
2. Authenticated AMI calls need `X-CSRFTOKEN` **and** a matching
   `__Host-garc` cookie; mismatch → 401.
3. The ASRock-style `/api/settings/fan/*` fan endpoints are **absent** here —
   `Invalid API Call`. Don't chase them; use Redfish.
4. `__Host-garc` is a host-only cookie on `<BMC_IP>` — replicate it exactly.
5. Use `curl -k` (self-signed cert) everywhere.
6. **PUT/OPTIONS are 501 on Fanprofile** — don't plan profile or duty writes;
   only the `SetFanMode` action (Auto/Half/Full) is writable.
