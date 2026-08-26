import { useEffect, useState } from 'react'
import type { Snapshot, FanProfile } from '../types'
import type { RowCfg } from '../rowconfig'
import { WidgetShell } from './shell'
import { Bar } from './CpuWidget'
import { EditableRows } from './EditableRows'
import * as api from '../api'

export function FansWidget({
  snap,
  admin,
  readOnly,
  dryRun,
  monitor,
  dutyOverride,
  edit,
  rowsCfg,
  onRowCfg,
}: {
  snap: Snapshot
  admin: boolean
  readOnly: boolean
  dryRun: boolean
  monitor: boolean
  dutyOverride?: boolean
  edit?: boolean
  rowsCfg?: RowCfg
  onRowCfg?: (c: RowCfg) => void
}) {
  const [profiles, setProfiles] = useState<FanProfile[]>([])
  const [active, setActive] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)

  // Monitor mode is display-only: no writes (even as admin). The server
  // enforces it; the UI reflects it. Duty override may be unavailable on the
  // board (verified: BMC rejects PUT on Fanprofile).
  const canWrite = admin && !readOnly && !monitor
  const canDuty = canWrite && (dutyOverride ?? true)

  const load = async () => {
    try {
      const p = await api.getProfiles()
      // The BMC can return an empty/absent list; never allow null into state
      // (the map below would crash).
      setProfiles(p && Array.isArray(p) ? p : [])
      const a = await api.getActiveProfile()
      setActive(a?.active ?? '')
    } catch (e) {
      setMessage((e as Error).message)
    }
  }
  useEffect(() => {
    load()
  }, [])

  if (monitor) {
    return (
      <WidgetShell title="Fans" action={<span className="pill pill-ok">Monitor</span>}>
        <div className="text-sm text-(--text-faint)">Display only — switch to Control to adjust fans.</div>
        <FanList snap={snap} edit={edit} rowsCfg={rowsCfg} onRowCfg={onRowCfg} />
      </WidgetShell>
    )
  }

  if (!admin) {
    return (
      <WidgetShell title="Fans">
        <div className="text-sm text-(--text-faint)">Read-only — sign in as admin to control fans.</div>
        <FanList snap={snap} edit={edit} rowsCfg={rowsCfg} onRowCfg={onRowCfg} />
      </WidgetShell>
    )
  }

  const chooseMode = async (mode: string) => {
    setBusy(true)
    setMessage('')
    try {
      await api.setFanMode(mode)
      setActive(mode)
      setMessage(`Fan mode → ${mode}${dryRun ? ' (dry-run, no write)' : ''}`)
    } catch (e) {
      setMessage((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const setDuty = async (fan: number, duty: number) => {
    setBusy(true)
    setMessage('')
    try {
      await api.setFanDuty(fan, duty)
      setMessage(`Fan ${fan} duty → ${Math.round(duty)}%${dryRun ? ' (dry-run)' : ''}`)
    } catch (e) {
      setMessage((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const modes = ['Auto', 'Half', 'Full']

  return (
    <WidgetShell
      title="Fans"
      action={readOnly ? <span className="pill pill-warn">Read-only</span> : dryRun ? <span className="pill pill-warn">Dry-run</span> : undefined}
    >
      <div className="space-y-3">
        <div className="flex flex-wrap gap-1.5">
          {modes.map((m) => (
            <button
              key={m}
              onClick={() => chooseMode(m)}
              disabled={busy}
              className={`px-2.5 py-1 rounded-full text-xs font-semibold border transition ${
                m === active
                  ? 'border-(--accent) bg-(--accent) text-(--bg)'
                  : 'border-(--border) bg-(--bg-panel-2) text-(--text-muted) hover:text-(--text)'
              }`}
            >
              {m}
            </button>
          ))}
        </div>
        {profiles.length > 0 && (
          <div className="text-[10px] text-(--text-faint)">
            Profile: {profiles.map((p) => p.name).join(' · ')}
          </div>
        )}

        {message && <div className="text-xs text-(--accent-2)">{message}</div>}
        {!canWrite && !readOnly && <div className="text-xs text-(--warn)">Dry-run active — changes are logged but not written.</div>}

        <FanList snap={snap} onDuty={canDuty ? setDuty : undefined} busy={busy} edit={edit} rowsCfg={rowsCfg} onRowCfg={onRowCfg} />

        <GpuFans snap={snap} canWrite={canWrite} />
      </div>
    </WidgetShell>
  )
}

function FanList({
  snap,
  onDuty,
  busy,
  edit,
  rowsCfg,
  onRowCfg,
}: {
  snap: Snapshot
  onDuty?: (fan: number, duty: number) => Promise<void>
  busy?: boolean
  edit?: boolean
  rowsCfg?: RowCfg
  onRowCfg?: (c: RowCfg) => void
}) {
  const fans = snap.fans ?? []
  const cfg = rowsCfg ?? { hidden: {}, names: {}, order: [] }
  if (fans.length === 0) return <div className="text-sm text-(--text-faint)">No fan sensors reported.</div>
  return (
    <EditableRows
      rows={fans.map((f) => ({ id: String(f.id), label: f.name }))}
      cfg={cfg}
      edit={!!edit}
      canRename
      onChange={(c) => onRowCfg?.(c)}
      render={(id, label) => {
        const f = fans.find((x) => String(x.id) === id)!
        return <FanRow fan={f} label={label} onDuty={onDuty} busy={busy} />
      }}
    />
  )
}

// FanRow renders one fan. Duty is NOT reported by the BMC (only RPM + a max
// speed range, which doesn't correlate to duty) — so when unknown we show
// "auto" instead of a fabricated number. Only once the user sets a duty via the
// slider do we display the requested %.
function FanRow({
  fan,
  label,
  onDuty,
  busy,
}: {
  fan: { id: number; name: string; rpm: number; duty: number; autoDuty: number }
  label: string
  onDuty?: (fan: number, duty: number) => Promise<void>
  busy?: boolean
}) {
  const [requested, setRequested] = useState<number | null>(null)
  const idle = fan.rpm <= 0
  // Duty logic: a value we've set (requested) beats a reported duty (mock) beats
  // the estimated auto-duty from the profile curve. If none match, unknown.
  const reported = fan.duty > 0 ? Math.round(fan.duty) : null
  let shown: number | null = requested ?? reported
  if (shown === null && fan.autoDuty > 0) {
    shown = Math.round(fan.autoDuty)
  }
  const isAuto = requested === null && reported === null && fan.autoDuty > 0

  return (
    <div className="rounded-lg bg-(--bg-panel-2) p-2">
      <div className="flex justify-between text-xs">
        <span className="text-(--text-muted)">
          {label} <span className="text-(--text-faint)">#{fan.id}</span>
          {idle && <span className="pill pill-warn ml-1">idle</span>}
        </span>
        <span className="mono text-(--text)">
          {Math.round(fan.rpm)} rpm
          {shown !== null ? ` · ${shown}%` : ' · auto'}
        </span>
      </div>
      {onDuty && (
        <div className="mt-1 flex items-center gap-2">
          <input
            type="range"
            min={0}
            max={100}
            step={5}
            value={requested ?? reported ?? 0}
            disabled={busy || idle}
            onChange={(e) => {
              const v = Number(e.target.value)
              setRequested(v)
              onDuty(fan.id, v)
            }}
            className="flex-1"
          />
          <span className="mono text-xs w-16 text-right">
            {requested !== null
              ? `set ${requested}%`
              : isAuto
                ? `auto ~${Math.round(fan.autoDuty)}%`
                : reported !== null
                  ? `${reported}%`
                  : 'auto'}
          </span>
        </div>
      )}
    </div>
  )
}

function GpuFans({ snap, canWrite }: { snap: Snapshot; canWrite: boolean }) {
  const gpus = (snap.gpus ?? []).filter((g) => g.fanPct > 0 || g.fanControl)
  if (gpus.length === 0) return null
  return (
    <div>
      <div className="label mb-1">GPU fans</div>
      <div className="space-y-1.5">
        {gpus.map((g) => (
          <div key={g.index} className="flex items-center gap-2 text-xs">
            <span className="w-16 truncate text-(--text-muted)">GPU {g.index}</span>
            <div className="flex-1">
              <Bar pct={g.fanPct} color="var(--accent)" />
            </div>
            <span className="mono w-10 text-right">{Math.round(g.fanPct)}%</span>
          </div>
        ))}
        {!canWrite && <div className="text-[10px] text-(--text-faint)">GPU fan writes require admin (and may be locked on this card).</div>}
      </div>
    </div>
  )
}
