import { useRef, useState, type ReactNode } from 'react'
import type { RowCfg } from '../rowconfig'
import { applyOrder } from '../rowconfig'

// EditableRows wraps a list of rows with drag-reorder, hide, and optional
// rename controls — only rendered when `edit` is true. Children are called per
// row with the row's id + display name so the widget renders its own content
// while this component owns the edit chrome.
//
// In edit mode ALL rows are shown (hidden ones dimmed) so the user can restore
// them; outside edit mode hidden rows are filtered out.
//
// Reordering uses pointer events (not HTML5 drag-and-drop) because GridStack
// installs its own drag handling on the grid that swallows native DnD — rows
// must be dragable independently of widget dragging.
export function EditableRows({
  rows,
  cfg,
  edit,
  canRename,
  onChange,
  render,
}: {
  rows: { id: string; label: string }[]
  cfg: RowCfg
  edit: boolean
  canRename?: boolean
  onChange: (next: RowCfg) => void
  render: (id: string, label: string) => ReactNode
}) {
  const [dragId, setDragId] = useState<string | null>(null)
  const [dragOverId, setDragOverId] = useState<string | null>(null)
  const dragging = useRef(false)

  const ordered = applyOrder(rows, cfg.order)
  const shown = edit ? ordered : ordered.filter((r) => !cfg.hidden[r.id])
  const hiddenCount = ordered.filter((r) => cfg.hidden[r.id]).length

  const merge = (patch: Partial<RowCfg>) => onChange({ ...cfg, ...patch })

  const toggleHidden = (id: string) => {
    merge({ hidden: { ...cfg.hidden, [id]: !cfg.hidden[id] } })
  }

  const rename = (id: string, name: string) => {
    merge({ names: { ...cfg.names, [id]: name } })
  }

  // Reorder: move `dragId` to `targetId`'s slot. Rebuilds canonical order
  // (visible first, then any hidden leftovers).
  const reorder = (dragId: string, targetId: string) => {
    if (dragId === targetId) return
    const ids = shown.map((r) => r.id)
    const from = ids.indexOf(dragId)
    const to = ids.indexOf(targetId)
    if (from < 0 || to < 0) return
    ids.splice(to, 0, ids.splice(from, 1)[0])
    const hiddenLeft = cfg.order.filter((id) => !ids.includes(id))
    merge({ order: [...ids, ...hiddenLeft] })
  }

  // Pointer-based drag: on grip pointerdown, record the dragged row and attach
  // window move/up listeners. The drop target is the row under the pointer
  // (elementFromPoint) — rows are hit-tested by their wrapper element.
  const startDrag = (e: React.PointerEvent, id: string) => {
    e.preventDefault()
    e.stopPropagation()
    dragging.current = true
    setDragId(id)

    const onMove = (ev: PointerEvent) => {
      if (!dragging.current) return
      const el = document.elementFromPoint(ev.clientX, ev.clientY)
      const rowEl = el?.closest('[data-row-id]') as HTMLElement | null
      if (rowEl) {
        setDragOverId(rowEl.dataset.rowId ?? null)
      }
    }
    const onUp = (ev: PointerEvent) => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      const el = document.elementFromPoint(ev.clientX, ev.clientY)
      const rowEl = el?.closest('[data-row-id]') as HTMLElement | null
      const target = rowEl?.dataset.rowId
      if (target) reorder(id, target)
      dragging.current = false
      setDragId(null)
      setDragOverId(null)
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
  }

  if (shown.length === 0) {
    return (
      <div className="text-sm text-(--text-faint)">
        All rows hidden{edit ? ' — click ✕ again to restore' : '.'}
      </div>
    )
  }

  return (
    <div className="space-y-1.5">
      {shown.map((r) => {
        const isHidden = !!cfg.hidden[r.id]
        const label = cfg.names[r.id]?.trim() || r.label
        return (
          <div
            key={r.id}
            data-row-id={r.id}
            className={`group transition ${
              dragOverId === r.id && dragId !== r.id ? 'ring-1 ring-(--accent-2)' : ''
            } ${dragId === r.id ? 'opacity-50' : ''}`}
          >
            {edit && (
              <div className="flex items-center gap-1 mb-0.5">
                <span
                  className="cursor-grab select-none text-(--text-faint) hover:text-(--accent-2) touch-none"
                  onPointerDown={(e) => startDrag(e, r.id)}
                  title="Drag to reorder"
                >
                  ⠿
                </span>
                {canRename ? (
                  <input
                    className="flex-1 min-w-0 bg-(--bg-panel-2) border border-(--border) rounded px-1.5 py-0.5 text-[10px] focus:border-(--accent)"
                    value={label}
                    placeholder={r.label}
                    onChange={(e) => rename(r.id, e.target.value)}
                  />
                ) : (
                  <span className="flex-1 text-[10px] text-(--text-muted) truncate">{label}</span>
                )}
                <span className="text-[10px] text-(--text-faint)">{r.id}</span>
                <button
                  className="px-1 rounded text-[10px] text-(--text-faint) hover:text-(--accent-2) hover:bg-(--bg-hover)"
                  onClick={() => toggleHidden(r.id)}
                  title={isHidden ? 'Show row' : 'Hide row'}
                >
                  {isHidden ? '👁' : '✕'}
                </button>
              </div>
            )}
            <div className={isHidden && !edit ? 'opacity-30' : isHidden && edit ? 'opacity-40' : ''}>
              {render(r.id, label)}
            </div>
          </div>
        )
      })}
      {edit && hiddenCount > 0 && (
        <div className="text-[10px] text-(--text-faint) pt-1">
          {hiddenCount} hidden {hiddenCount === 1 ? 'row' : 'rows'} — click 👁 to restore
        </div>
      )}
    </div>
  )
}
