import type { Snapshot } from '../types'
import { Sparkline } from './Sparkline'
import { formatBytes } from '../types'
import { snapTime } from './shell'

export function SummaryWidget({ snap, series }: { snap: Snapshot; series: (number | null)[] }) {
  const cpu = snap.cpu ?? {}
  const gpuCount = snap.gpus?.length ?? 0
  const maxGpuTemp = snap.gpus?.reduce((m, g) => Math.max(m, g.temp), 0) ?? 0
  const gpuLoad = snap.gpus?.reduce((s, g) => s + g.util, 0) ?? 0
  const gpuLoadAvg = gpuCount ? gpuLoad / gpuCount : 0
  const memPct = cpu.memTotal ? (cpu.memUsed! / cpu.memTotal) * 100 : 0

  return (
    <div className="grid grid-cols-2 md:grid-cols-5 gap-3 px-1 py-1">
      <KPI
        label="CPU Load"
        value={`${Math.round(cpu.loadPct ?? 0)}%`}
        series={series}
        color="var(--accent)"
      />
      <KPI
        label="GPU Temp"
        value={`${Math.round(maxGpuTemp)}°C`}
        series={series}
        color="var(--danger)"
      />
      <KPI
        label="GPU Load"
        value={`${Math.round(gpuLoadAvg)}%`}
        series={series}
        color="var(--accent-2)"
      />
      <KPI
        label="Memory"
        value={formatBytes(cpu.memUsed ?? 0)}
        sub={cpu.memTotal ? `${Math.round(memPct)}% of ${formatBytes(cpu.memTotal)}` : undefined}
      />
      <div className="flex flex-col justify-center">
        <span className="label">{gpuCount > 0 ? `${gpuCount}× GPU online` : 'no GPUs'}</span>
        <span className="mono text-[11px] text-(--text-faint)">{snapTime(snap)}</span>
      </div>
    </div>
  )
}

function KPI({ label, value, sub, series, color }: { label: string; value: string; sub?: string; series?: (number | null)[]; color?: string }) {
  return (
    <div className="flex items-center gap-3 rounded-lg bg-(--bg-panel-2) px-3 py-2">
      <div>
        <div className="label">{label}</div>
        <div className="mono text-xl font-semibold text-(--text)">{value}</div>
        {sub && <div className="mono text-[10px] text-(--text-faint)">{sub}</div>}
      </div>
      {series && color && <div className="ml-auto"><Sparkline points={series} color={color} width={64} height={24} /></div>}
    </div>
  )
}
