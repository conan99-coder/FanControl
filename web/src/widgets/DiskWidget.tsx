import type { Snapshot } from '../types'
import type { RowCfg } from '../rowconfig'
import type { Thresholds } from '../api'
import { WidgetShell } from './shell'
import { Bar } from './CpuWidget'
import { formatBytes, formatRate } from '../types'
import { EditableRows } from './EditableRows'

// DiskWidget shows mounted filesystems (volumes). Physical NVMe drive hardware
// (name/serial/size/temp) lives in the separate DrivesWidget.
export function DiskWidget({
  snap,
  edit,
  rowsCfg,
  onRowCfg,
  thresholds,
}: {
  snap: Snapshot
  edit?: boolean
  rowsCfg?: RowCfg
  onRowCfg?: (c: RowCfg) => void
  thresholds?: Thresholds
}) {
  const disks = snap.disks ?? []
  const cfg = rowsCfg ?? { hidden: {}, names: {}, order: [] }
  const warnPct = thresholds?.diskUsedWarn ?? 90
  return (
    <WidgetShell title="Disk volumes">
      {disks.length === 0 ? (
        <div className="text-sm text-(--text-faint)">No volumes detected.</div>
      ) : (
        <EditableRows
          rows={disks.map((d) => ({ id: d.mount, label: d.mount }))}
          cfg={cfg}
          edit={!!edit}
          onChange={(c) => onRowCfg?.(c)}
          render={(id) => {
            const d = disks.find((x) => x.mount === id)!
            const usedPct = d.totalBytes ? ((d.totalBytes - d.freeBytes) / d.totalBytes) * 100 : 0
            const warn = usedPct > warnPct
            return (
              <div className="rounded-lg bg-(--bg-panel-2) p-2">
                <div className="flex justify-between items-baseline">
                  <span className="text-xs text-(--text-muted)">{d.mount}</span>
                  <span className="mono text-xs text-(--text)">{formatBytes(d.totalBytes - d.freeBytes)} / {formatBytes(d.totalBytes)}</span>
                </div>
                <div className="mt-1 flex items-center gap-2">
                  <div className="flex-1">
                    <Bar pct={usedPct} color={warn ? 'var(--warn)' : 'var(--accent-2)'} />
                  </div>
                  <span className={`mono text-xs ${warn ? 'text-(--warn)' : 'text-(--text-muted)'}`}>{usedPct.toFixed(0)}%</span>
                </div>
                <div className="mt-1 flex justify-between text-xs text-(--text-faint)">
                  <span className="mono">R {formatRate(d.readRate)}</span>
                  <span className="mono">W {formatRate(d.writeRate)}</span>
                </div>
              </div>
            )
          }}
        />
      )}
    </WidgetShell>
  )
}
