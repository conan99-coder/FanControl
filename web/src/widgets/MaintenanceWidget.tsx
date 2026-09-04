import { useState } from 'react'
import type { Snapshot, VastRig } from '../types'
import * as api from '../api'
import { WidgetShell } from './shell'

// MaintenanceWidget: per-machine maintenance state (always visible) + a
// schedule form in Control mode. One tab per machine (tab name includes the
// machine id).
const CATEGORIES = ['power', 'internet', 'disk', 'gpu', 'software', 'other']

export function MaintenanceWidget({
  snap,
  admin,
  monitor,
  readOnly,
  dryRun,
}: {
  snap: Snapshot
  admin?: boolean
  monitor?: boolean
  readOnly?: boolean
  dryRun?: boolean
}) {
  const rigs = snap.vastRigs ?? []
  const [active, setActive] = useState(0)
  const idx = Math.min(active, Math.max(rigs.length - 1, 0))
  const rig = rigs[idx]
  const canWrite = !!admin && !monitor && !readOnly

  return (
    <WidgetShell title="Maintenance" icon={<span>🔧</span>}>
      {rigs.length === 0 ? (
        <div className="text-sm text-(--text-faint)">No Vast.ai machine data — enable the `vast` provider.</div>
      ) : (
        <>
          <div className="flex gap-1.5 flex-wrap mb-2">
            {rigs.map((r, i) => (
              <button
                key={r.id}
                onClick={() => setActive(i)}
                className={`px-2 py-0.5 rounded-full text-[10px] border transition ${
                  i === idx
                    ? 'border-(--accent) bg-(--accent) text-(--bg)'
                    : 'border-(--border) text-(--text-muted) hover:text-(--text)'
                }`}
              >
                {r.hostname} #{r.id}
              </button>
            ))}
          </div>
          <MachineMaintenance key={rig.id} rig={rig} canWrite={canWrite} dryRun={dryRun} />
        </>
      )}
    </WidgetShell>
  )
}

function MachineMaintenance({ rig, canWrite, dryRun }: { rig: VastRig; canWrite: boolean; dryRun?: boolean }) {
  const [start, setStart] = useState('')
  const [duration, setDuration] = useState('2')
  const [category, setCategory] = useState('other')
  const [msg, setMsg] = useState('')

  const schedule = async () => {
    setMsg('')
    const unix = start ? Math.floor(Date.parse(start) / 1000) : 0
    const hours = Number(duration)
    if (!unix || !hours) {
      setMsg('✗ start time and duration required')
      return
    }
    try {
      await api.scheduleVastMaintenance(rig.id, unix, hours, category)
      setMsg(dryRun ? 'logged only (dry-run)' : 'scheduled ✓')
    } catch (e) {
      setMsg('✗ ' + (e as Error).message)
    }
  }

  return (
    <div className="space-y-2 text-xs">
      {rig.maintenance ? (
        <div className="pill pill-warn">{rig.maintenance}</div>
      ) : (
        <div className="text-[10px] text-(--text-faint)">No maintenance scheduled.</div>
      )}
      {!canWrite ? (
        <div className="text-[10px] text-(--text-faint)">Switch to Control mode to schedule maintenance.</div>
      ) : (
        <>
          <label className="block">
            <div className="text-[10px] text-(--text-muted) mb-0.5">Start (local time)</div>
            <input type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} className={inp} />
          </label>
          <label className="block">
            <div className="text-[10px] text-(--text-muted) mb-0.5">Duration (hours)</div>
            <input type="number" step="0.5" min="0.5" value={duration} onChange={(e) => setDuration(e.target.value)} className={inp} />
          </label>
          <label className="block">
            <div className="text-[10px] text-(--text-muted) mb-0.5">Category</div>
            <select value={category} onChange={(e) => setCategory(e.target.value)} className={inp}>
              {CATEGORIES.map((c) => (
                <option key={c} value={c}>{c}</option>
              ))}
            </select>
          </label>
          <div className="flex items-center gap-2 flex-wrap">
            <button className={btnPrimary} onClick={schedule}>Schedule maintenance</button>
            {msg && <span className="text-[10px] text-(--text-muted)">{msg}</span>}
          </div>
        </>
      )}
    </div>
  )
}

const inp = 'w-full rounded-md border border-(--border) bg-(--bg-panel-2) px-2 py-1 text-xs outline-none focus:border-(--accent)'
const btnPrimary = 'px-3 py-1.5 rounded-md bg-(--accent) text-xs font-semibold text-(--bg) hover:opacity-90'
