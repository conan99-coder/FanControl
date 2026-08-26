import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import 'gridstack/dist/gridstack.min.css'
// Extra CSS defines the .gs-<n> column width classes needed for responsive
// column switching (columnOpts.breakpointForWindow). Without it items collapse
// to zero width at narrower breakpoints.
import 'gridstack/dist/gridstack-extra.min.css'
import { GridStack } from 'gridstack'
import type { Snapshot, DMeta } from './types'
import { Login } from './Login'
import { ErrorBoundary } from './ErrorBoundary'
import * as api from './api'
import type { RowConfigs, RowCfg } from './rowconfig'
import { loadRowConfigs, saveRowConfigs, cfgFor } from './rowconfig'
import { SummaryWidget } from './widgets/SummaryWidget'
import { CpuWidget } from './widgets/CpuWidget'
import { GpuWidget } from './widgets/GpuWidget'
import { DiskWidget } from './widgets/DiskWidget'
import { DrivesWidget } from './widgets/DrivesWidget'
import { NetWidget } from './widgets/NetWidget'
import { VoltsWidget } from './widgets/VoltsWidget'
import { TempsWidget } from './widgets/TempsWidget'
import { FansWidget } from './widgets/FansWidget'

export type WidgetType = 'summary' | 'cpu' | 'gpu' | 'disk' | 'drives' | 'net' | 'temps' | 'fans' | 'volts'

interface WidgetDef {
  id: string
  type: WidgetType
  gpu?: number
}

interface AppState {
  user: string
  role: string
}

export default function App() {
  const [auth, setAuth] = useState<AppState | null>(null)
  const [snap, setSnap] = useState<Snapshot | null>(null)
  const [history, setHistory] = useState<(number | null)[]>([])
  const [status, setStatus] = useState<api.Status | null>(null)
  const [discovery, setDiscovery] = useState<DMeta[]>([])
  const [theme, setTheme] = useState<'dark' | 'light'>(() => (localStorage.getItem('fc-theme') as 'dark' | 'light') || 'dark')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [metaLoaded, setMetaLoaded] = useState(false)
  const [modeBusy, setModeBusy] = useState(false)
  const [editMode, setEditMode] = useState(false)
  const [rowsCfg, setRowsCfg] = useState<RowConfigs>(() => loadRowConfigs())
  const gridRef = useRef<HTMLDivElement>(null)
  const gsRef = useRef<GridStack | null>(null)
  const historyRef = useRef<(number | null)[]>([])

  const role = auth?.role
  const admin = role === 'admin'
  const ready = !!snap

  // Row config change handler: persists to localStorage immediately.
  const setRowCfg = (widget: string, next: RowCfg) => {
    setRowsCfg((prev) => {
      const updated = { ...prev, [widget]: next }
      saveRowConfigs(updated)
      return updated
    })
  }

  // Monitor mode: display-only. The server enforces it (writes return 403), the
  // UI reflects it. Defaults to Monitor on the server; we mirror its state.
  const monitor = status?.monitor ?? true

  const toggleMode = async () => {
    if (!admin || modeBusy) return
    setModeBusy(true)
    try {
      const next = await api.setMode(!monitor)
      setStatus((s) => (s ? { ...s, monitor: next.monitor } : s))
    } catch (e) {
      // If the server refuses (e.g. auth), status will re-sync on next poll.
      console.warn('mode switch failed', e)
    } finally {
      setModeBusy(false)
    }
  }

  // On first load, ask the server whether auth is required. When auth is off,
  // auto-enter as anonymous admin so the dashboard renders without a login.
  useEffect(() => {
    api
      .getMeta()
      .then((m) => {
        if (!m.auth_enabled) {
          setAuth({ role: 'admin', user: 'anonymous' })
        }
      })
      .catch(() => {
        // Meta unreachable (e.g. server restarting) — leave logged out.
      })
      .finally(() => setMetaLoaded(true))
  }, [])

  // Theme
  useEffect(() => {
    document.documentElement.classList.toggle('theme-light', theme === 'light')
    localStorage.setItem('fc-theme', theme)
  }, [theme])

  // SSE live data
  useEffect(() => {
    if (!auth) return
    const close = api.streamMetrics((s) => {
      setSnap(s)
      // Rolling history for the summary sparkline (CPU load series).
      const load = s.cpu.loadPct ?? 0
      const arr = [...historyRef.current, load]
      if (arr.length > 50) arr.shift()
      historyRef.current = arr
      setHistory(arr)
    })
    return close
  }, [auth])

  // Non-SSE data (status, discovery) polled periodically
  useEffect(() => {
    if (!auth) return
    const tick = () => {
      api.getStatus().then(setStatus).catch(() => {})
      api.getDiscovery().then(setDiscovery).catch(() => {})
    }
    tick()
    const t = setInterval(tick, 8000)
    return () => clearInterval(t)
  }, [auth])

  // Widget grid
  const widgets: WidgetDef[] = [
    { id: 'summary', type: 'summary' },
    { id: 'gpu0', type: 'gpu', gpu: 0 },
    { id: 'gpu1', type: 'gpu', gpu: 1 },
    { id: 'cpu', type: 'cpu' },
    { id: 'fans', type: 'fans' },
    { id: 'temps', type: 'temps' },
    { id: 'drives', type: 'drives' },
    { id: 'disk', type: 'disk' },
    { id: 'net', type: 'net' },
    { id: 'volts', type: 'volts' },
  ]

  // Initialize gridstack once the grid div is actually in the DOM (i.e. after
  // the first snapshot arrives and `ready` becomes true). Keying on `ready`
  // (not `auth`) is essential: auth is set before any data snapshot, so the grid
  // wasn't mounted yet and gridRef.current would be null.
  //
  // GridStack owns the item DOM (addWidget) and React widgets are rendered into
  // those nodes via createPortal. This avoids the React-vs-gridstack DOM fight
  // (children replaced on re-render collapsing the layout).
  const gridInitStarted = useRef(false)
  const widgetMounts = useRef<Map<string, HTMLElement>>(new Map())
  const [gridVersion, setGridVersion] = useState(0) // re-render after mounts
  useEffect(() => {
    if (!ready || gridInitStarted.current || !gridRef.current) return
    gridInitStarted.current = true
    const gs = GridStack.init(
      {
        column: 12,
        // Responsive: with a narrow window the grid reflows into fewer columns
        // so widgets stay readable instead of squashing into 12 thin columns.
        columnOpts: {
          breakpointForWindow: true,
          breakpoints: [
            { w: 900, c: 6, layout: 'moveScale' },
            { w: 600, c: 4, layout: 'moveScale' },
            { w: 420, c: 3, layout: 'moveScale' },
          ],
        },
        cellHeight: 64,
        margin: 6,
        float: true,
        resizable: { handles: 'se' },
        // Drag widgets by their header only (not the content), so row-level
        // drags inside a widget (reorder grips) move rows, not the widget.
        draggable: {
          handle: '.gs-widget-drag',
          // Pause (ms) before collision re-layout occurs while hovering over
          // other widgets — prevents a quick pass-over from rearranging the
          // layout; nearby widgets only make room after ~1s of hover.
          pause: 1000,
        },
      },
      gridRef.current
    )
    gsRef.current = gs

    // Layout resolution: the widget list is the source of truth for which
    // widgets exist; saved positions (localStorage) overlay default positions so
    // newly added widgets (e.g. 'drives') still appear even if the browser saved
    // an older layout without them.
    const defaults = defaultLayout()
    const saved = loadLayout()
    const layout = widgets.map((w) => {
      const savedNode = saved.find((s) => s.id === w.id)
      const def = defaults.find((d) => d.id === w.id)
      return (
        savedNode ?? {
          id: w.id,
          x: def?.x ?? 0,
          y: def?.y ?? 0,
          w: def?.w ?? 4,
          h: def?.h ?? 3,
        }
      )
    })
    for (const w of layout) {
      // addWidget creates the grid-stack-item element; the content option
      // becomes the inner HTML of its .grid-stack-item-content child. We portal
      // the React widget into that child afterwards.
      const item = gs.addWidget({
        id: w.id,
        x: w.x,
        y: w.y,
        w: w.w,
        h: w.h,
        content: '',
      })
      const content = item.querySelector('.grid-stack-item-content') as HTMLElement | null
      if (content) {
        widgetMounts.current.set(w.id, content)
      }
    }
    setGridVersion((v) => v + 1)

    gs.on('change', () => saveLayout())
    return () => {
      gs.destroy()
      gsRef.current = null
      gridInitStarted.current = false
      widgetMounts.current.clear()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ready])

  // Default positions for the widget grid (kept in sync with the config layout).
  function defaultLayout(): { id: string; x: number; y: number; w: number; h: number }[] {
    // Heights are tuned to the content at cellHeight 64 so each widget fits its
    // data and the overall grid snaps into a coherent, mostly-uniform layout.
    return [
      { id: 'summary', x: 0, y: 0, w: 12, h: 1 },
      { id: 'gpu0', x: 0, y: 1, w: 4, h: 3 },
      { id: 'gpu1', x: 4, y: 1, w: 4, h: 3 },
      { id: 'cpu', x: 8, y: 1, w: 4, h: 3 },
      { id: 'fans', x: 0, y: 4, w: 6, h: 4 },
      { id: 'temps', x: 6, y: 4, w: 6, h: 4 },
      { id: 'drives', x: 0, y: 8, w: 6, h: 3 },
      { id: 'disk', x: 6, y: 8, w: 6, h: 3 },
      { id: 'volts', x: 0, y: 11, w: 6, h: 2 },
      { id: 'net', x: 6, y: 11, w: 6, h: 2 },
    ]
  }

  // Layout persistence (browser-side; server config update comes later).
  function loadLayout(): { id: string; x: number; y: number; w: number; h: number }[] {
    try {
      return JSON.parse(localStorage.getItem('fc-layout') || '[]')
    } catch {
      return []
    }
  }
  function saveLayout() {
    const gs = gsRef.current
    if (!gs) return
    const nodes = gs.getGridItems().map((el) => {
      const n = el.gridstackNode
      // addWidget sets the id on the gridstackNode; prefer it over the attribute.
      const id = (el as HTMLElement & { id?: string }).id || n?.id || el.getAttribute('data-gs-id') || ''
      return { id, x: n?.x ?? 0, y: n?.y ?? 0, w: n?.w ?? 1, h: n?.h ?? 1 }
    })
    localStorage.setItem('fc-layout', JSON.stringify(nodes))
  }

  const renderWidget = (w: WidgetDef) => {
    if (!snap) return null
    switch (w.type) {
      case 'summary':
        return <SummaryWidget snap={snap} series={history} />
      case 'cpu':
        return <CpuWidget snap={snap} />
      case 'gpu':
        return <GpuWidget snap={snap} index={w.gpu ?? 0} />
      case 'disk':
        return <DiskWidget snap={snap} edit={editMode} rowsCfg={cfgFor(rowsCfg, 'disk')} onRowCfg={(c) => setRowCfg('disk', c)} />
      case 'drives':
        return <DrivesWidget snap={snap} edit={editMode} rowsCfg={cfgFor(rowsCfg, 'drives')} onRowCfg={(c) => setRowCfg('drives', c)} />
      case 'volts':
        return <VoltsWidget snap={snap} edit={editMode} rowsCfg={cfgFor(rowsCfg, 'volts')} onRowCfg={(c) => setRowCfg('volts', c)} />
      case 'net':
        return <NetWidget snap={snap} edit={editMode} rowsCfg={cfgFor(rowsCfg, 'net')} onRowCfg={(c) => setRowCfg('net', c)} />
      case 'temps':
        return <TempsWidget snap={snap} edit={editMode} rowsCfg={cfgFor(rowsCfg, 'temps')} onRowCfg={(c) => setRowCfg('temps', c)} />
      case 'fans':
        return <FansWidget snap={snap} admin={admin} readOnly={status?.read_only ?? false} dryRun={status?.dry_run ?? false} monitor={monitor} dutyOverride={status?.capabilities?.dutyOverride ?? true} edit={editMode} rowsCfg={cfgFor(rowsCfg, 'fans')} onRowCfg={(c) => setRowCfg('fans', c)} />
      default:
        return null
    }
  }

  // Wait for the meta fetch to settle before deciding what to show, so the
  // login screen never flashes when auth is disabled.
  if (!metaLoaded) {
    return null
  }
  if (!auth) {
    return <Login onLogin={(r, name) => setAuth({ role: r, user: name })} />
  }

  const goRed = status?.governor_tripped

  return (
    <div className="min-h-screen p-3">
      {/* Header */}
      <header className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span className="text-xl">🌡️</span>
          <h1 className="text-lg font-semibold">FanControl</h1>
          <span className="label ml-2">Rig monitor</span>
          {goRed && <span className="pill pill-danger animate-pulse-danger">Safety governor</span>}
        </div>
        <div className="flex items-center gap-3">
          {admin && (
            <button
              onClick={toggleMode}
              disabled={modeBusy}
              title={monitor ? 'Switch to Control mode (fan writes allowed)' : 'Switch to Monitor mode (display only, never writes)'}
              className={`px-3 py-1 rounded-full text-xs font-semibold border transition ${
                monitor
                  ? 'border-(--accent) bg-(--accent) text-(--bg)'
                  : 'border-(--warn) bg-(--bg-panel-2) text-(--warn) hover:opacity-90'
              }`}
            >
              {monitor ? '🛰️ Monitor' : '⚙️ Control'}
            </button>
          )}
          <button
            onClick={() => setEditMode((v) => !v)}
            title={editMode ? 'Finish editing the dashboard' : 'Edit dashboard: hide, rename, reorder rows'}
            className={`px-3 py-1 rounded-full text-xs font-semibold border transition ${
              editMode
                ? 'border-(--warn) bg-(--warn) text-(--bg)'
                : 'border-(--border) bg-(--bg-panel-2) text-(--text-muted) hover:text-(--text)'
            }`}
          >
            {editMode ? '✔ Done' : '✏️ Edit'}
          </button>
          {admin && (
            <button
              onClick={() => setSettingsOpen((v) => !v)}
              className="text-xs text-(--text-muted) hover:text-(--text) underline"
            >
              Settings
            </button>
          )}
          <button
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
            className="text-xs text-(--text-muted) hover:text-(--text)"
          >
            {theme === 'dark' ? '☀️ Light' : '🌙 Dark'}
          </button>
          <span className="text-xs text-(--text-faint)">{auth.user} ({role})</span>
          <button
            onClick={async () => { await api.logout().catch(() => {}); setAuth(null) }}
            className="text-xs text-(--text-muted) hover:text-(--danger)"
          >
            Sign out
          </button>
        </div>
      </header>

      {/* Settings drawer */}
      {admin && settingsOpen && (
        <div className="mb-3 rounded-xl border border-(--border) bg-(--bg-panel) p-4 text-sm">
          <div className="label mb-2">Detected hardware</div>
          {discovery.length === 0 ? (
            <div className="text-(--text-faint)">Polling discovery…</div>
          ) : (
            discovery.map((d) => (
              <div key={d.source} className="mb-2 text-xs">
                <span className="font-semibold text-(--accent-2)">{d.source}</span>
                {d.cpu?.model && <span className="text-(--text-muted)"> — {d.cpu.model} ({d.cpu.cores}c/{d.cpu.threads}t)</span>}
                <div className="text-(--text-faint) mt-1">
                  {d.fans?.length > 0 && <>fans: {d.fans.map((f) => `${f.name}#${f.id}`).join(', ')} · </>}
                  {d.gpus?.length > 0 && <>gpus: {d.gpus.map((g) => `${g.index}: ${g.name}${g.fan_control ? ' [fan-cfg]' : ''}`).join(', ')} · </>}
                  {d.thermals?.length > 0 && <>thermals: {d.thermals.length} · </>}
                  {d.disks?.length > 0 && <>disks: {d.disks.length} · </>}
                  {d.nets?.length > 0 && <>nets: {d.nets.join(', ')}</>}
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {/* Edit-mode hint */}
      {editMode && (
        <div className="mb-3 rounded-xl border border-(--warn) bg-(--bg-panel) px-4 py-2 text-xs text-(--text-muted)">
          <span className="font-semibold text-(--warn)">Edit mode</span> — drag ⠿ to reorder rows, click ✕ to hide, type a custom
          name (fans/temps). Changes save automatically. Click <b>✔ Done</b> to finish.
        </div>
      )}

      {/* Grid — gridstack owns the item DOM; React widgets are portaled into it. */}
      {!ready ? (
        <div className="text-center text-(--text-faint) py-20">Connecting to live data…</div>
      ) : (
        <div ref={gridRef} className="grid-stack" />
      )}

      {/* React widgets live inside gridstack-owned nodes via portals. They mount
          only after gridstack created the slots (gridVersion), and re-render
          automatically as snapshots stream in. */}
      {gridVersion > 0 &&
        widgets.map((w) => {
          const target = widgetMounts.current.get(w.id)
          if (!target) return null
          return createPortal(
            <ErrorBoundary>{snap && renderWidget(w)}</ErrorBoundary>,
            target
          )
        })}

      <footer className="text-center text-[10px] text-(--text-faint) mt-4">
        {monitor ? 'Monitor mode — display only' : status?.read_only ? 'Read-only' : status?.dry_run ? 'Dry-run — reads live, writes logged only' : 'Control mode — writes active'}
        {snap && <span className="ml-1">· {new Date(snap.time).toLocaleTimeString()}</span>}
      </footer>
    </div>
  )
}
