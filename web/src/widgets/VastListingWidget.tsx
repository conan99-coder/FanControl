import { useState } from 'react'
import type { Snapshot, VastRig } from '../types'
import { fmtUSD, fmtDateTime } from '../types'
import * as api from '../api'
import { WidgetShell } from './shell'

// VastListingWidget: host-side listing per machine — values are always
// visible (read-only in Monitor mode), editable in Control mode. One tab per
// machine (tab name includes the machine id).
function toLocalInput(unix: number): string {
  if (!unix) return ''
  const d = new Date(unix * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// cleanNumber renders a float without trailing zeroes (10 -> "10", 9.765625 ->
// "9.765625"). Used for the $/TB bandwidth values.
function cleanNumber(n: number): string {
  if (!isFinite(n) || n === 0) return ''
  return String(Number(n.toFixed(6)))
}

export function VastListingWidget({
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
    <WidgetShell title="Vast listing" icon={<span>🏷️</span>}>
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
          <MachineListing key={rig.id} rig={rig} canWrite={canWrite} dryRun={dryRun} />
        </>
      )}
    </WidgetShell>
  )
}

function MachineListing({ rig, canWrite, dryRun }: { rig: VastRig; canWrite: boolean; dryRun?: boolean }) {
  // Keyed per machine: initial values come straight from the rig data.
  const [gpu, setGpu] = useState(() => (rig.listedGpuCost ? String(rig.listedGpuCost) : ''))
  const [disk, setDisk] = useState(() => (rig.listedStorageCost ? String(rig.listedStorageCost) : ''))
  const [inetUp, setInetUp] = useState(() => cleanNumber(rig.listedInetUpCost * 1024))
  const [inetDown, setInetDown] = useState(() => cleanNumber(rig.listedInetDownCost * 1024))
  const [minBid, setMinBid] = useState(() => (rig.minBidPrice ? String(rig.minBidPrice) : ''))
  const [endDate, setEndDate] = useState(() => toLocalInput(rig.endDate))
  const [msg, setMsg] = useState('')

  if (!canWrite) {
    // Read-only view: same values, no controls.
    return (
      <div className="grid grid-cols-2 gap-x-4 gap-y-0.5 text-xs">
        <Row label="OnDemand" value={`${fmtUSD(rig.listedGpuCost)}/h·GPU`} />
        <Row label="Disk" value={`${fmtUSD(rig.listedStorageCost)}/GB/mo`} />
        <Row label="BW up" value={`${fmtUSD(rig.listedInetUpCost * 1024)}/TB`} />
        <Row label="BW down" value={`${fmtUSD(rig.listedInetDownCost * 1024)}/TB`} />
        <Row label="Min bid" value={`${fmtUSD(rig.minBidPrice)}/h`} />
        <Row label="Listing expires" value={fmtDateTime(rig.endDate)} />
      </div>
    )
  }

  const num = (s: string) => (s.trim() === '' ? undefined : Number(s))
  const save = async () => {
    setMsg('')
    try {
      const endUnix = endDate ? Math.floor(Date.parse(endDate) / 1000) : undefined
      await api.updateVastListing(rig.id, {
        priceGpu: num(gpu),
        priceDisk: num(disk),
        // Bandwidth is shown/edited in $/TB; the API expects $/GB.
        priceInetUp: num(inetUp) !== undefined ? num(inetUp)! / 1024 : undefined,
        priceInetDown: num(inetDown) !== undefined ? num(inetDown)! / 1024 : undefined,
        priceMinBid: num(minBid),
        endDateUnix: endUnix,
      })
      setMsg(dryRun ? 'logged only (dry-run)' : 'saved ✓')
    } catch (e) {
      setMsg('✗ ' + (e as Error).message)
    }
  }

  const unlist = async () => {
    if (!confirm(`Unlist machine ${rig.hostname}? It disappears from search immediately.`)) return
    setMsg('')
    try {
      await api.unlistVastMachine(rig.id)
      setMsg(dryRun ? 'logged only (dry-run)' : 'unlisted ✓')
    } catch (e) {
      setMsg('✗ ' + (e as Error).message)
    }
  }

  return (
    <div className="space-y-2 text-xs">
      <Field label="OnDemand price ($/h · GPU)">
        <input type="number" step="0.1" min="0" value={gpu} onChange={(e) => setGpu(e.target.value)} className={inp} />
      </Field>
      <Field label="Disk price ($/GB/month)">
        <input type="number" step="0.05" min="0" value={disk} onChange={(e) => setDisk(e.target.value)} className={inp} />
      </Field>
      <div className="grid grid-cols-2 gap-2">
        <Field label="Bandwidth up ($/TB)">
          <input type="number" step="1" min="0" value={inetUp} onChange={(e) => setInetUp(e.target.value)} className={inp} />
        </Field>
        <Field label="Bandwidth down ($/TB)">
          <input type="number" step="1" min="0" value={inetDown} onChange={(e) => setInetDown(e.target.value)} className={inp} />
        </Field>
      </div>
      <Field label="Minimum bid price ($/h · GPU)">
        <input type="number" step="0.1" min="0" value={minBid} onChange={(e) => setMinBid(e.target.value)} className={inp} />
      </Field>
      <Field label="Listing expiration date" hint="The machine disappears from search after this date (automatic unlist).">
        <input type="datetime-local" value={endDate} onChange={(e) => setEndDate(e.target.value)} className={inp} />
      </Field>
      <div className="flex items-center gap-2 flex-wrap">
        <button className={btnPrimary} onClick={save}>Save listing</button>
        <button className={btnDanger} onClick={unlist}>Unlist now</button>
        {msg && <span className="text-[10px] text-(--text-muted)">{msg}</span>}
      </div>
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-(--text-faint)">{label}</span>
      <span className="mono text-(--text)">{value}</span>
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <div className="text-[10px] text-(--text-muted) mb-0.5">{label}</div>
      {children}
      {hint && <div className="text-[9px] text-(--text-faint) mt-0.5">{hint}</div>}
    </label>
  )
}

const inp = 'w-full rounded-md border border-(--border) bg-(--bg-panel-2) px-2 py-1 text-xs outline-none focus:border-(--accent)'
const btnPrimary = 'px-3 py-1.5 rounded-md bg-(--accent) text-xs font-semibold text-(--bg) hover:opacity-90'
const btnDanger = 'px-3 py-1.5 rounded-md border border-(--danger) text-xs text-(--danger) hover:bg-(--danger) hover:text-(--bg)'
