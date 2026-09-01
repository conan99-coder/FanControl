import type { ReactNode } from 'react'
import type { Snapshot } from '../types'

// WidgetShell is the common frame for a dashboard widget: a titled card with an
// optional warning badge and an optional action slot (e.g. a fan control).
export function WidgetShell({
  title,
  icon,
  warn,
  warnSoft,
  children,
  action,
}: {
  title: string
  icon?: ReactNode
  warn?: string
  warnSoft?: string
  children: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="panel flex flex-col h-full">
      {/* .gs-widget-drag: gridstack drags the whole widget ONLY from this
          header, so reorder grips inside the content work independently. */}
      <div className="gs-widget-drag flex items-center justify-between px-3 py-2 border-b border-(--border) bg-(--bg-panel-2) cursor-grab select-none">
        <div className="flex items-center gap-2">
          {icon && <span className="text-(--accent)">{icon}</span>}
          <span className="label">{title}</span>
        </div>
        <div className="flex items-center gap-2 cursor-default">
          {warnSoft && <span className="pill pill-warn text-[9px] leading-none px-1.5 py-0.5">{warnSoft}</span>}
          {warn && (
            <span className="pill pill-danger animate-pulse-danger text-[9px] leading-none px-1.5 py-0.5">{warn}</span>
          )}
          {action}
        </div>
      </div>
      <div className="flex-1 overflow-auto p-3">{children}</div>
    </div>
  )
}

// threshold returns a warn message if a value crosses a limit.
export function warnIf(v: number, limit: number, label: string): string | undefined {
  if (limit > 0 && v >= limit) return `${label} ${Math.round(v)}°C`
  return undefined
}

// snapTime formats the snapshot timestamp.
export function snapTime(s: Snapshot): string {
  try {
    return new Date(s.time).toLocaleTimeString()
  } catch {
    return ''
  }
}
