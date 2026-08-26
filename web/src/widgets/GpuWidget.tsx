import type { Snapshot } from '../types'
import { WidgetShell, warnIf } from './shell'
import { Bar, Metric } from './CpuWidget'
import { formatBytes } from '../types'

export function GpuWidget({ snap, index }: { snap: Snapshot; index: number }) {
  const gpu = snap.gpus?.find((g) => g.index === index)
  if (!gpu) {
    return (
      <WidgetShell title={`GPU ${index}`}>
        <div className="text-sm text-(--text-faint)">No GPU at index {index}.</div>
      </WidgetShell>
    )
  }
  const warn = warnIf(gpu.temp, gpu.maxTemp, 'GPU')
  const utilPct = gpu.util ?? 0
  const vramPct = gpu.vramTotal ? (gpu.vramUsed / gpu.vramTotal) * 100 : 0

  return (
    <WidgetShell title={`GPU ${index} — ${gpu.name}`} warn={warn}>
      <div className="space-y-3">
        <div className="flex justify-between items-baseline">
          <span className="mono text-3xl font-semibold text-(--accent)">
            {Math.round(gpu.temp)}<span className="text-base text-(--text-muted)">°C</span>
          </span>
          <span className="mono text-sm text-(--text-muted)">{fmtPower(gpu.power, gpu.powerLimit)}</span>
        </div>

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
