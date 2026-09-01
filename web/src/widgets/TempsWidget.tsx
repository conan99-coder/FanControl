import type { Snapshot } from '../types'
import type { RowCfg } from '../rowconfig'
import type { Thresholds } from '../api'
import { displayName } from '../rowconfig'
import { WidgetShell } from './shell'
import { Bar } from './CpuWidget'
import { EditableRows } from './EditableRows'

export function TempsWidget({
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
  const thermals = snap.thermals ?? []
  const cfg = rowsCfg ?? { hidden: {}, names: {}, order: [] }
  const warnAt = thresholds?.gpuTempWarn
  const hardAt = thresholds?.gpuTempHard
  return (
    <WidgetShell title="Temperatures">
      {thermals.length === 0 ? (
        <div className="text-sm text-(--text-faint)">No thermal sensors.</div>
      ) : (
        <EditableRows
          rows={thermals.map((t) => ({ id: String(t.id), label: t.name }))}
          cfg={cfg}
          edit={!!edit}
          canRename
          onChange={(c) => onRowCfg?.(c)}
          render={(id, label) => {
            const t = thermals.find((x) => String(x.id) === id)!
            // Warning state from the config thresholds (same gates as the temp
            // graph); fall back to the BMC's own sensor max otherwise.
            const hot = hardAt != null ? t.temp >= hardAt : t.max > 0 && t.temp / t.max > 0.85
            const warn = warnAt != null && t.temp >= warnAt && !hot
            const pct = t.max ? Math.min(100, (t.temp / t.max) * 100) : Math.min(100, t.temp)
            return (
              <div className="flex items-center gap-2">
                <span className="w-32 truncate text-xs text-(--text-muted)">{displayName(cfg, id, label)}</span>
                <div className="flex-1">
                  <Bar pct={pct} color={hot ? 'var(--danger)' : warn ? 'var(--warn)' : 'var(--accent)'} />
                </div>
                <span className={`mono text-xs w-12 text-right ${hot ? 'text-(--danger)' : warn ? 'text-(--warn)' : 'text-(--text)'}`}>
                  {Math.round(t.temp)}°C
                </span>
              </div>
            )
          }}
        />
      )}
    </WidgetShell>
  )
}
