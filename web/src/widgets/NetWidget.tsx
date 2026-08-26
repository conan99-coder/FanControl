import type { Snapshot } from '../types'
import type { RowCfg } from '../rowconfig'
import { WidgetShell } from './shell'
import { formatRate } from '../types'
import { EditableRows } from './EditableRows'

export function NetWidget({
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
  const nets = (snap.nets ?? []).filter((n) => n.interface !== 'lo')
  const cfg = rowsCfg ?? { hidden: {}, names: {}, order: [] }
  return (
    <WidgetShell title="Network">
      {nets.length === 0 ? (
        <div className="text-sm text-(--text-faint)">No interfaces.</div>
      ) : (
        <EditableRows
          rows={nets.map((n) => ({ id: n.interface, label: n.interface }))}
          cfg={cfg}
          edit={!!edit}
          onChange={(c) => onRowCfg?.(c)}
          render={(id) => {
            const n = nets.find((x) => x.interface === id)!
            return (
              <div className="rounded-lg bg-(--bg-panel-2) p-2 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className={`w-2 h-2 rounded-full ${n.up ? 'bg-(--ok)' : 'bg-(--text-faint)'}`} />
                  <span className="text-xs text-(--text-muted)">{n.interface}</span>
                </div>
                <div className="flex gap-3">
                  <div className="text-right">
                    <div className="text-[10px] text-(--text-faint) uppercase">Down</div>
                    <div className="mono text-xs text-(--accent-2)">{formatRate(n.rxRate)}</div>
                  </div>
                  <div className="text-right">
                    <div className="text-[10px] text-(--text-faint) uppercase">Up</div>
                    <div className="mono text-xs text-(--accent)">{formatRate(n.txRate)}</div>
                  </div>
                </div>
              </div>
            )
          }}
        />
      )}
    </WidgetShell>
  )
}
