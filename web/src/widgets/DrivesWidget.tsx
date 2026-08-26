import type { Snapshot } from '../types'
import type { RowCfg } from '../rowconfig'
import { displayName } from '../rowconfig'
import { WidgetShell } from './shell'
import { formatBytes } from '../types'
import { EditableRows } from './EditableRows'

// DrivesWidget lists physical NVMe drives with model, serial, size and
// temperature — hardware, not mounted volumes.
export function DrivesWidget({
  snap,
  edit,
  rowsCfg,
  onRowCfg,
}: {
  snap: Snapshot
  edit?: boolean
  rowsCfg?: RowCfg
  onRowCfg?: (c: RowCfg) => void
}) {
  const drives = snap.drives ?? []
  const cfg = rowsCfg ?? { hidden: {}, names: {}, order: [] }
  return (
    <WidgetShell title="Drives (NVMe)">
      {drives.length === 0 ? (
        <div className="text-sm text-(--text-faint)">No NVMe drives detected.</div>
      ) : (
        <EditableRows
          rows={drives.map((d) => ({ id: d.device, label: d.model || d.device }))}
          cfg={cfg}
          edit={!!edit}
          onChange={(c) => onRowCfg?.(c)}
          render={(id, label) => {
            const d = drives.find((x) => x.device === id)!
            const hot = d.temp > 65
            return (
              <div className="rounded-lg bg-(--bg-panel-2) p-2">
                <div className="flex items-center justify-between">
                  <span className="mono text-[10px] text-(--text-faint)">{d.device}</span>
                  <span className={`mono text-xs rounded-full px-2 py-0.5 ${
                    hot ? 'bg-(--warn) text-(--text)' : 'bg-(--bg-hover) text-(--accent-2)'
                  }`}>
                    {d.temp > 0 ? `${Math.round(d.temp)}°C` : '—'}
                  </span>
                </div>
                <div className="mt-0.5 text-sm text-(--text) font-medium truncate">{displayName(cfg, id, label)}</div>
                <div className="flex justify-between mt-1 text-xs text-(--text-faint)">
                  <span className="mono truncate">SN {d.serial || '—'}</span>
                  <span className="mono whitespace-nowrap">{d.sizeBytes ? formatBytes(d.sizeBytes) : '—'}</span>
                </div>
                {d.firmware && (
                  <div className="text-[10px] text-(--text-faint) mt-0.5">FW {d.firmware}</div>
                )}
              </div>
            )
          }}
        />
      )}
    </WidgetShell>
  )
}
