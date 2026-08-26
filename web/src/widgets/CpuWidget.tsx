import type { Snapshot } from '../types'
import { WidgetShell, warnIf } from './shell'
import { formatBytes } from '../types'

export function CpuWidget({ snap }: { snap: Snapshot }) {
  const cpu = snap.cpu ?? {}
  const load = cpu.loadPct ?? 0
  const memPct = cpu.memTotal ? (cpu.memUsed ?? 0) / cpu.memTotal * 100 : 0
  const warn = warnIf(cpu.cpuTemp ?? 0, cpu.cpuTempMax ?? 0, 'CPU')
  const perCore = cpu.perCoreLoad ?? []

  return (
    <WidgetShell title="CPU" warn={warn}>
      <div className="space-y-3">
        <div className="flex justify-between items-baseline">
          <span className="mono text-3xl font-semibold text-(--accent)">
            {load.toFixed(0)}<span className="text-base text-(--text-muted)">%</span>
          </span>
          <span className="mono text-sm text-(--text-muted)">{cpu.model ?? '—'}</span>
        </div>

        <div className="grid grid-cols-2 gap-3 text-sm">
          <Metric label="Temp" value={`${Math.round(cpu.cpuTemp ?? 0)}°C`} />
          <Metric label="Cores" value={`${cpu.cores ?? 0}c / ${cpu.threads ?? 0}t`} />
        </div>

        <div>
          <div className="flex justify-between text-xs text-(--text-muted) mb-1">
            <span>Memory</span>
            <span className="mono">{formatBytes(cpu.memUsed ?? 0)} / {formatBytes(cpu.memTotal ?? 0)}</span>
          </div>
          <Bar pct={memPct} color="var(--accent-2)" />
        </div>

        {perCore.length > 0 && (
          <div>
            <div className="text-xs text-(--text-muted) mb-1">Per-core</div>
            <div className="grid gap-1" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(4px, 1fr))' }}>
              {perCore.map((v, i) => (
                <div key={i} className="h-8 rounded-sm" style={{ background: `color-mix(in srgb, var(--accent-2) ${Math.min(100, v)}%, var(--grid-bg))` }} title={`core ${i}: ${v.toFixed(0)}%`} />
              ))}
            </div>
          </div>
        )}
      </div>
    </WidgetShell>
  )
}

export function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-(--bg-panel-2) px-2 py-1.5">
      <div className="label">{label}</div>
      <div className="mono text-base text-(--text)">{value}</div>
    </div>
  )
}

export function Bar({ pct, color }: { pct: number; color: string }) {
  return (
    <div className="h-2 rounded-full bg-(--bg-panel-2) overflow-hidden">
      <div className="h-full rounded-full transition-all" style={{ width: `${Math.min(100, Math.max(0, pct))}%`, background: color }} />
    </div>
  )
}
