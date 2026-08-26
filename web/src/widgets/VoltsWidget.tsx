import type { Snapshot } from '../types'
import type { RowCfg } from '../rowconfig'
import { WidgetShell } from './shell'
import { EditableRows } from './EditableRows'

// KindVolts matches the Go metrics enum value for voltage scalars.
const KindVolts = 7

// VoltsWidget shows the BMC power rails (P_12V, P_5V, P_3V3, ...) read from the
// AMI sensor API — these are only available there, not via Redfish Thermal.
export function VoltsWidget({
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
  const volts = (snap.extra ?? []).filter((s) => s.kind === KindVolts)
  const cfg = rowsCfg ?? { hidden: {}, names: {}, order: [] }
  if (volts.length === 0) {
    return (
      <WidgetShell title="Voltages">
        <div className="text-sm text-(--text-faint)">No voltage sensors reported.</div>
      </WidgetShell>
    )
  }
  return (
    <WidgetShell title="Voltages">
      <EditableRows
        rows={volts.map((v) => ({ id: v.name, label: v.name }))}
        cfg={cfg}
        edit={!!edit}
        onChange={(c) => onRowCfg?.(c)}
        render={(id) => {
          const v = volts.find((x) => x.name === id)!
          const pct = v.max > v.min ? ((v.value - v.min) / (v.max - v.min)) * 100 : 50
          const outOfRange = v.min > 0 && (v.value < v.min || v.value > v.max)
          return (
            <div className="rounded-lg bg-(--bg-panel-2) px-2 py-1.5">
              <div className="flex justify-between items-baseline">
                <span className="label">{v.name}</span>
                <span className={`mono text-sm ${outOfRange ? 'text-(--warn)' : 'text-(--accent-2)'}`}>
                  {v.value.toFixed(2)}V
                </span>
              </div>
              <div className="h-1 rounded-full bg-(--bg-hover) mt-1 overflow-hidden">
                <div
                  className="h-full rounded-full"
                  style={{
                    width: `${Math.min(100, Math.max(0, pct))}%`,
                    background: outOfRange ? 'var(--warn)' : 'var(--accent-strong)',
                  }}
                />
              </div>
            </div>
          )
        }}
      />
    </WidgetShell>
  )
}
