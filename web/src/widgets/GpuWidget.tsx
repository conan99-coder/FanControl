import { useState } from 'react'
import type { Snapshot } from '../types'
import type { Thresholds } from '../api'
import * as api from '../api'
import { WidgetShell, warnIf } from './shell'
import { Bar, Metric } from './CpuWidget'
import { formatBytes } from '../types'

// Fixed power-limit choices (the card's range is validated by the driver).
const POWER_CHOICES = [
  { w: 400, label: '400W' },
  { w: 500, label: '500W' },
  { w: 600, label: '600W (no limit)' },
]

export function GpuWidget({
  snap,
  index,
  thresholds,
  admin,
  monitor,
  readOnly,
  dryRun,
  gpuPowerControl,
}: {
  snap: Snapshot
  index: number
  thresholds?: Thresholds
  admin?: boolean
  monitor?: boolean
  readOnly?: boolean
  dryRun?: boolean
  gpuPowerControl?: boolean
}) {
  const gpu = snap.gpus?.find((g) => g.index === index)
  const [powerMsg, setPowerMsg] = useState('')
  if (!gpu) {
    return (
      <WidgetShell title={`GPU ${index}`}>
        <div className="text-sm text-(--text-faint)">No GPU at index {index}.</div>
      </WidgetShell>
    )
  }
  const canWrite = !!admin && !monitor && !readOnly
  const setPower = async (watts: number) => {
    setPowerMsg('')
    try {
      await api.setGPUPower(gpu.index, watts)
      setPowerMsg(dryRun ? 'logged only (dry-run)' : 'applied ✓')
    } catch (e) {
      setPowerMsg('✗ ' + (e as Error).message)
    }
  }
  // Warning state from the config thresholds when available; fall back to the
  // card's own maxTemp otherwise.
  const warnAt = thresholds?.gpuTempWarn
  const hardAt = thresholds?.gpuTempHard
  let warn: string | undefined
  let warnSoft: string | undefined
  if (hardAt != null && gpu.temp >= hardAt) {
    warn = `hard ${Math.round(gpu.temp)}°`
  } else if (warnAt != null && gpu.temp >= warnAt) {
    warnSoft = `warn ${Math.round(gpu.temp)}°`
  } else if (warnAt == null && hardAt == null) {
    warn = warnIf(gpu.temp, gpu.maxTemp, 'GPU')
  }
  const utilPct = gpu.util ?? 0
  const vramPct = gpu.vramTotal ? (gpu.vramUsed / gpu.vramTotal) * 100 : 0

  return (
    <WidgetShell title={`GPU ${index} — ${gpu.name}`} warn={warn} warnSoft={warnSoft}>
      <div className="space-y-3">
        <div className="flex justify-between items-baseline">
          <span className="mono text-3xl font-semibold text-(--accent)">
            {Math.round(gpu.temp)}<span className="text-base text-(--text-muted)">°C</span>
          </span>
          <span className="mono text-sm text-(--text-muted)">{fmtPower(gpu.power, gpu.powerLimit)}</span>
        </div>

        {canWrite && gpuPowerControl && (
          <div className="flex items-center gap-2 text-xs">
            <span className="text-(--text-faint)">Power limit</span>
            <select
              value=""
              onChange={(e) => setPower(Number(e.target.value))}
              className="rounded-md border border-(--border) bg-(--bg-panel-2) px-1.5 py-0.5 text-xs outline-none focus:border-(--accent)"
              title={dryRun ? 'Dry-run: the change is logged, not applied' : `Current limit: ${Math.round(gpu.powerLimit)}W`}
            >
              <option value="" disabled>
                {Math.round(gpu.powerLimit)}W now
              </option>
              {POWER_CHOICES.map((c) => (
                <option key={c.w} value={c.w}>
                  {c.label}
                </option>
              ))}
            </select>
            {powerMsg && <span className="text-[10px] text-(--text-muted)">{powerMsg}</span>}
          </div>
        )}

        <div className="grid grid-cols-2 gap-3 text-sm">
          <Metric label="Util" value={`${Math.round(utilPct)}%`} />
          <Metric label="Fan" value={gpu.fanPct > 0 ? `${Math.round(gpu.fanPct)}%` : '—'} />
        </div>

        <div>
          <div className="flex justify-between text-xs text-(--text-muted) mb-1">
            <span>Utilization</span>
            <span className="mono">{Math.round(utilPct)}%</span>
          </div>
          <Bar pct={utilPct} color="var(--accent)" />
        </div>

        <div>
          <div className="flex justify-between text-xs text-(--text-muted) mb-1">
            <span>VRAM</span>
            <span className="mono">{formatBytes(gpu.vramUsed)} / {formatBytes(gpu.vramTotal)}</span>
          </div>
          <Bar pct={vramPct} color="var(--accent-2)" />
        </div>
      </div>
    </WidgetShell>
  )
}

function fmtPower(power: number, limit: number): string {
  if (!power) return '—'
  const s = Math.round(power)
  return limit ? `${s} / ${Math.round(limit)} W` : `${s} W`
}
