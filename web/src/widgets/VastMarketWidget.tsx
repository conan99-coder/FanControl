import type { Snapshot } from '../types'
import { fmtUSD } from '../types'
import { WidgetShell } from './shell'

// VastMarketWidget shows the Vast.ai GPU marketplace snapshot per GPU type
// (`vastai metrics gpu --verified true`): rented/available counts, current
// utilization, and the current price range. Read-only; the server applies the
// configured market filter.
export function VastMarketWidget({ snap }: { snap: Snapshot }) {
  const gpus = snap.vastGpus ?? []
  return (
    <WidgetShell title="GPU market" icon={<span>📊</span>}>
      {gpus.length === 0 ? (
        <div className="text-sm text-(--text-faint)">
          No market data — enable the `vast` provider in the config.
        </div>
      ) : (
        <div className="space-y-1.5">
          {gpus.map((g) => (
            <div key={g.name} className="rounded-lg bg-(--bg-panel-2) px-2.5 py-1.5">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-(--text) truncate">{g.name}</span>
                <span className="mono text-xs text-(--accent-2)">{fmtUSD(g.priceMedian)}/h</span>
              </div>
              <div className="flex justify-between text-[10px] text-(--text-faint) mt-0.5">
                <span>
                  <span className="mono text-(--text)">{g.rentedVerified}</span> rented ·{' '}
                  <span className="mono text-(--text)">{g.availVerified}</span> avail
                </span>
                <span>
                  util <span className="mono text-(--text)">{g.usage.toFixed(0)}%</span>
                </span>
                <span>
                  <span className="mono text-(--text)">{fmtUSD(g.priceP10)}</span>–
                  <span className="mono text-(--text)">{fmtUSD(g.priceP90)}</span>
                </span>
              </div>
            </div>
          ))}
        </div>
      )}
    </WidgetShell>
  )
}
