import type { Snapshot } from '../types'
import { formatBytes } from '../types'
import { WidgetShell } from './shell'

// DockerWidget shows read-only metadata of the Docker containers running on
// the rig — the renters' Vast instances (named C.<instance_id>). Shows name,
// image/template, status, and CPU/mem usage. Metadata only: it never touches
// container contents or logs (tenant data).
export function DockerWidget({ snap }: { snap: Snapshot }) {
  const containers = snap.containers ?? []
  return (
    <WidgetShell title="Docker instances" icon={<span>🐳</span>}>
      {containers.length === 0 ? (
        <div className="text-sm text-(--text-faint)">
          No containers reported — enable the `docker` provider in the config.
        </div>
      ) : (
        <div className="space-y-2">
          {containers.map((c) => (
            <div key={c.name} className="rounded-lg bg-(--bg-panel-2) px-2.5 py-2">
              <div className="flex items-center justify-between gap-2">
                <span className="mono text-xs text-(--text)">{c.name}</span>
                {c.status && <span className="text-[10px] text-(--text-muted)">{c.status}</span>}
              </div>
              <div className="text-xs text-(--text-muted) truncate mt-0.5" title={c.image}>
                {c.image}
              </div>
              <div className="flex justify-between text-[10px] text-(--text-faint) mt-1">
                <span>
                  CPU{' '}
                  <span className="mono text-(--text)">
                    {isFinite(c.cpusPct) ? `${c.cpusPct.toFixed(1)}% (${(c.cpusPct / 100).toFixed(2)} cores)` : '—'}
                  </span>
                </span>
                <span>
                  Mem{' '}
                  <span className="mono text-(--text)">
                    {c.memUsedBytes > 0 ? formatBytes(c.memUsedBytes) : '—'}
                    {c.memTotalBytes > 0 ? ` / ${formatBytes(c.memTotalBytes)}` : ''}
                  </span>
                </span>
              </div>
            </div>
          ))}
        </div>
      )}
    </WidgetShell>
  )
}
