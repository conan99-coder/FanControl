import type { Snapshot } from '../types'
import { fmtUSD, fmtDateTime } from '../types'
import { WidgetShell } from './shell'

// VastWidget shows read-only Vast.ai hosting telemetry per machine:
// earnings ($/h, $/day, ~$/week), listed rate, running rentals, and contract
// end dates. All data comes from the server's vast provider (`vastai show
// machines --raw`) — this widget has no write controls.
export function VastWidget({ snap }: { snap: Snapshot }) {
  const rigs = snap.vastRigs ?? []
  return (
    <WidgetShell title="Vast rigs" icon={<span>💰</span>}>
      {rigs.length === 0 ? (
        <div className="text-sm text-(--text-faint)">
          No Vast.ai machine data — enable the `vast` provider in the config to populate this.
        </div>
      ) : (
        <div className="space-y-2">
          {rigs.map((r) => {
            const nowSec = Date.now() / 1000
            const hrsLeft = r.clientEndDate > nowSec ? (r.clientEndDate - nowSec) / 3600 : 0
            // Guaranteed: keep earning at the current rate until the renter
            // contracts end. Potential: full machine at the listed rate.
            const guaranteed = r.earnHour * hrsLeft
            const potential = r.listedGpuCost * r.numGpus * hrsLeft
            const potentialRate = r.listedGpuCost * r.numGpus
            const hasEnd = r.clientEndDate > nowSec
            return (
              <div key={r.id} className="rounded-lg bg-(--bg-panel-2) px-2.5 py-2">
                <div className="flex items-center justify-between gap-2">
                  <span className="label">{r.hostname} · #{r.id}</span>
                  <span className="flex items-center gap-1.5">
                    {r.verification && (
                      <span className={`pill ${r.verification === 'verified' ? 'pill-ok' : 'pill-warn'}`}>
                        {r.verification}
                      </span>
                    )}
                    {r.reliability > 0 && (
                      <span className="mono text-[10px] text-(--text-faint)">{(r.reliability * 100).toFixed(1)}%</span>
                    )}
                  </span>
                </div>
                <div className="text-xs text-(--text-muted) mt-1">
                  {r.gpuName} × {r.numGpus}
                  {r.geolocation ? ` · ${r.geolocation}` : ''}
                  {r.rentalsRunning > 0 ? ` · ${r.rentalsRunning} rental${r.rentalsRunning > 1 ? 's' : ''} running` : ' · no rentals running'}
                </div>
                <div className="grid grid-cols-2 gap-x-4 gap-y-0.5 mt-1.5 text-xs">
                  <div className="flex justify-between">
                    <span className="text-(--text-faint)">Listed rate</span>
                    <span className="mono text-(--text)">{fmtUSD(r.listedGpuCost)}/h·GPU</span>
                  </div>
                  <div className="flex justify-between" title="Your accepted rate — earn_hour from Vast (what you're actually paid per hour)">
                    <span className="text-(--text-faint)">Accepted rate</span>
                    <span className="mono text-(--ok)">{fmtUSD(r.earnHour)}/h</span>
                  </div>
                  <div className="flex justify-between" title="Listed rate × GPUs (fully rented)">
                    <span className="text-(--text-faint)">Potential</span>
                    <span className="mono text-(--accent-2)">{fmtUSD(potentialRate)}/h</span>
                  </div>
                  <div className="flex justify-between" title="Vast's average daily earnings (earn_day from the CLI)">
                    <span className="text-(--text-faint)">Earnings average/day</span>
                    <span className="mono text-(--ok)">{fmtUSD(r.earnDay)}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-(--text-faint)">Renters end date</span>
                    <span className="mono text-(--text)">{fmtDateTime(r.clientEndDate)}</span>
                  </div>
                  <div className="flex justify-between" title="Live: accepted rate (earn_hour) × 24 — what a full day earns at your current rate">
                    <span className="text-(--text-faint)">Earnings current rate/day</span>
                    <span className="mono text-(--ok)">{fmtUSD(r.earnHour * 24)}</span>
                  </div>
                  <div className="pl-2 flex justify-between" title={`Guaranteed until the renter contracts end (current earn rate × hours left to ${fmtDateTime(r.clientEndDate)})`}>
                    <span className="text-(--text-faint)">Guaranteed</span>
                    <span className="mono text-(--ok)">{hasEnd ? fmtUSD(guaranteed) : '—'}</span>
                  </div>
                  <div className="flex justify-between" title="Vast's average weekly earnings (average/day × 7)">
                    <span className="text-(--text-faint)">Earnings average/week</span>
                    <span className="mono text-(--ok)">{fmtUSD(r.earnDay * 7)}</span>
                  </div>
                  <div className="pl-2 flex justify-between" title="Listed rate × GPUs × hours left until the renter contracts end">
                    <span className="text-(--text-faint)">Potential</span>
                    <span className="mono text-(--accent-2)">{hasEnd ? fmtUSD(potential) : '—'}</span>
                  </div>
                  <div />
                  <div className="flex justify-between">
                    <span className="text-(--text-faint)">Listing ends</span>
                    <span className="mono text-(--text)">{fmtDateTime(r.endDate)}</span>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </WidgetShell>
  )
}
