import { useEffect, useId, useMemo, useRef, useState, type RefObject } from 'react'
import type { Snapshot } from '../types'
import type { RowCfg } from '../rowconfig'
import { displayName, emptyCfg } from '../rowconfig'
import { WidgetShell } from './shell'
import * as api from '../api'

// Temperature history graph widget.
//
// - Plots the SAME thermal sensors as the Temperatures widget (snap.thermals).
// - Seeds from the server's /api/history once, then appends live SSE snapshots.
// - Legend chips toggle individual sensors on/off; the selection persists in
//   localStorage (fc-tempsgraph-hidden) so it survives reloads.
//
// Rendered with a hand-rolled SVG line chart (no chart dependency): each sensor
// gets its own trace + color from a fixed palette that reads on both themes.

// Sliding time window shown (5 minutes) and rolling buffer length.
const WINDOW_MS = 5 * 60 * 1000
const MAX_POINTS = 260
// The graph's OWN hide state (independent from the Temperatures widget):
// - UNHIDDEN: sensors unselected in edit mode — hidden from normal mode.
// - LINES: line toggles — dimmed chip, line off, chip stays visible.
const HIDDEN_KEY = 'fc-tempsgraph-hidden'
const LINES_KEY = 'fc-tempsgraph-lines'

// Palette (24 distinct hues) — saturated mid-tones, readable on dark + light.
const PALETTE = [
  '#38bdf8', '#f472b6', '#a78bfa', '#34d399', '#fbbf24', '#fb7185',
  '#22d3ee', '#c084fc', '#4ade80', '#fb923c', '#e879f9', '#2dd4bf',
  '#facc15', '#818cf8', '#f87171', '#67e8f9', '#d8b4fe', '#86efac',
  '#fdba74', '#f9a8d4', '#93c5fd', '#fca5a5', '#bef264', '#5eead4',
]

interface Pt {
  t: number // epoch ms
  vals: Record<string, number> // thermal id -> temp °C
}

function toPt(s: Snapshot): Pt | null {
  const t = Date.parse(s.time)
  if (!isFinite(t)) return null
  const vals: Record<string, number> = {}
  for (const th of s.thermals ?? []) vals[String(th.id)] = th.temp
  return { t, vals }
}

function loadSet(key: string): Set<string> {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return new Set()
    const arr = JSON.parse(raw)
    return new Set(Array.isArray(arr) ? arr : [])
  } catch {
    return new Set()
  }
}

function fmtTime(t: number): string {
  return new Date(t).toLocaleTimeString()
}

// useSize tracks the rendered width+height of an element so the chart expands
// to fill the widget (both axes), not a fixed size.
function useSize(): [RefObject<HTMLDivElement>, { w: number; h: number }] {
  const ref = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ w: 0, h: 0 })
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        const cr = e.contentRect
        setSize({ w: Math.round(cr.width), h: Math.round(cr.height) })
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, size]
}

function LineChart({
  pts,
  ids,
  hidden,
  colors,
  warn,
  hard,
}: {
  pts: Pt[]
  ids: string[]
  hidden: Set<string>
  colors: Map<string, string>
  warn?: number
  hard?: number
}) {
  const clipId = useId()
  const [ref, size] = useSize()
  const M = { l: 42, r: 12, t: 10, b: 20 }
  const W = size.w > 0 ? size.w : 640
  const H = size.h > 0 ? size.h : 190
  const plotW = W - M.l - M.r
  const plotH = H - M.t - M.b

  // A sensor is drawn in the chart unless its line is toggled off in the
  // legend. (Sensors unselected in edit mode are filtered out before this by
  // passing only row-visible ids.)
  const visible = ids.filter((id) => !hidden.has(id))

  if (pts.length < 2) {
    return (
      <div ref={ref} className="flex items-center justify-center h-full w-full text-xs text-(--text-faint)">
        Collecting…
      </div>
    )
  }

  const tMax = pts[pts.length - 1].t
  const tMin = Math.max(pts[0].t, tMax - WINDOW_MS)
  const inWin = pts.filter((p) => p.t >= tMin)

  const anyData = visible.some((id) => inWin.some((p) => p.vals[id] != null && isFinite(p.vals[id])))
  if (!anyData || visible.length === 0) {
    return (
      <div ref={ref} className="flex items-center justify-center h-full w-full text-xs text-(--text-faint)">
        {visible.length === 0 ? 'All sensors hidden — click a chip to show it.' : 'Collecting…'}
      </div>
    )
  }

  // Fixed range 30-100°C (temps below 30 are not expected): the graph stays
  // comparable over time.
  const dmin = 30
  const dmax = 100
  const ticks = [30, 50, 70, 90, 100]

  const spanT = Math.max(tMax - tMin, 1)
  const spanV = dmax - dmin || 1
  const xFor = (t: number) => M.l + ((t - tMin) / spanT) * plotW
  const yFor = (v: number) => M.t + (1 - (v - dmin) / spanV) * plotH

  // Build one polyline per visible sensor; break segments on missing samples.
  const traces = visible.map((id) => {
    const segs: [number, number][][] = []
    let cur: [number, number][] = []
    for (const p of inWin) {
      const v = p.vals[id]
      if (v == null || !isFinite(v)) {
        if (cur.length > 1) segs.push(cur)
        cur = []
        continue
      }
      cur.push([xFor(p.t), yFor(v)])
    }
    if (cur.length > 1) segs.push(cur)
    const last = cur.length > 0 ? cur[cur.length - 1] : null
    return { id, segs, last }
  })

  const midT = (tMin + tMax) / 2

  return (
    <div ref={ref} className="w-full h-full">
      <svg width={W} height={H} className="block">
        <defs>
          <clipPath id={clipId}>
            <rect x={M.l} y={M.t} width={plotW} height={plotH} />
          </clipPath>
        </defs>
        {/* threshold background bands: safe (green) -> warn (yellow) -> hard
            (red). Low opacity so the temperature lines stay readable. */}
        <g clipPath={`url(#${clipId})`}>
          {warn != null && warn > dmin && warn <= dmax && (
            <rect x={M.l} y={yFor(dmin)} width={plotW} height={Math.max(0, yFor(warn) - yFor(dmin))} fill="var(--ok)" opacity={0.12} />
          )}
          {warn != null && hard != null && hard > warn && warn < dmax && (
            <rect x={M.l} y={yFor(warn)} width={plotW} height={yFor(hard) - yFor(warn)} fill="var(--warn)" opacity={0.2} />
          )}
          {hard != null && hard > dmin && hard < dmax && (
            <rect x={M.l} y={yFor(hard)} width={plotW} height={yFor(dmax) - yFor(hard)} fill="var(--danger)" opacity={0.22} />
          )}
        </g>
        {/* horizontal grid + y labels */}
        {ticks.map((v) => (
          <g key={v}>
            <line x1={M.l} x2={W - M.r} y1={yFor(v)} y2={yFor(v)} stroke="var(--border)" strokeOpacity={0.5} strokeWidth={1} />
            <text x={M.l - 6} y={yFor(v) + 3} textAnchor="end" fontSize={9} fill="var(--text-faint)">
              {Math.round(v)}°
            </text>
          </g>
        ))}
        {/* x time labels */}
        <text x={xFor(tMin)} y={H - 5} fontSize={9} fill="var(--text-faint)">{fmtTime(tMin)}</text>
        <text x={xFor(midT)} y={H - 5} textAnchor="middle" fontSize={9} fill="var(--text-faint)">{fmtTime(midT)}</text>
        <text x={xFor(tMax)} y={H - 5} textAnchor="end" fontSize={9} fill="var(--text-faint)">{fmtTime(tMax)}</text>
        {/* threshold lines (warn yellow / hard red) */}
        {warn != null && warn > dmin && warn < dmax && (
          <g>
            <line x1={M.l} x2={W - M.r} y1={yFor(warn)} y2={yFor(warn)} stroke="var(--warn)" strokeOpacity={0.85} strokeWidth={1} strokeDasharray="6 4" />
            <text x={W - M.r} y={yFor(warn) - 4} textAnchor="end" fontSize={9} fill="var(--warn)">
              warn {Math.round(warn)}°
            </text>
          </g>
        )}
        {hard != null && hard > dmin && hard < dmax && (
          <g>
            <line x1={M.l} x2={W - M.r} y1={yFor(hard)} y2={yFor(hard)} stroke="var(--danger)" strokeOpacity={0.85} strokeWidth={1} strokeDasharray="6 4" />
            <text x={W - M.r} y={yFor(hard) - 4} textAnchor="end" fontSize={9} fill="var(--danger)">
              hard {Math.round(hard)}°
            </text>
          </g>
        )}
        {/* traces */}
        <g clipPath={`url(#${clipId})`}>
          {traces.map((tr) => {
            const color = colors.get(tr.id) ?? '#888'
            return (
              <g key={tr.id}>
                {tr.segs.map((s, i) => (
                  <path
                    key={i}
                    d={s.map(([x, y], j) => `${j === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`).join(' ')}
                    fill="none"
                    stroke={color}
                    strokeWidth={1.5}
                    strokeLinejoin="round"
                    strokeLinecap="round"
                  />
                ))}
                {tr.last && <circle cx={tr.last[0]} cy={tr.last[1]} r={2.5} fill={color} />}
              </g>
            )
          })}
        </g>
      </svg>
    </div>
  )
}

export function TempsGraphWidget({
  snap,
  edit,
  rowsCfg,
  thresholds,
}: {
  snap: Snapshot
  edit?: boolean
  rowsCfg?: RowCfg
  thresholds?: { gpuTempWarn: number; gpuTempHard: number; cpuTempWarn: number; cpuTempHard: number }
}) {
  const bufRef = useRef<Pt[]>([])
  const [buf, setBuf] = useState<Pt[]>([])
  // The graph's own hide state — completely independent from the Temperatures
  // widget (which keeps its own rows config on fc-rows["temps"]).
  const [hidden, setHiddenState] = useState<Set<string>>(() => loadSet(HIDDEN_KEY)) // unselected (edit mode)
  const [lines, setLinesState] = useState<Set<string>>(() => loadSet(LINES_KEY)) // line toggles (chip stays)
  const bootRef = useRef(false)
  // The Temperatures widget's row config is used READ-ONLY here, for custom
  // sensor names (renames show in both widgets). Hiding stays separate.
  const cfg = rowsCfg ?? emptyCfg()

  // Seed the buffer from server-side history once (pre-existing samples).
  useEffect(() => {
    if (bootRef.current) return
    bootRef.current = true
    api
      .getHistory()
      .then((h) => {
        const pts: Pt[] = []
        for (const s of h) {
          const p = toPt(s)
          if (p) pts.push(p)
        }
        const cur = bufRef.current
        if (cur.length === 0) {
          bufRef.current = pts
          setBuf(pts)
        } else {
          // Live appends already started: keep history points older than the
          // current buffer and append them in front.
          const cutoff = cur[0].t
          const merged = [...pts.filter((p) => p.t < cutoff), ...cur].slice(-MAX_POINTS)
          bufRef.current = merged
          setBuf(merged)
        }
      })
      .catch(() => {})
  }, [])

  // Append each live snapshot (deduped by timestamp).
  useEffect(() => {
    const p = toPt(snap)
    if (!p) return
    const last = bufRef.current[bufRef.current.length - 1]
    if (last && p.t <= last.t) return
    const arr = [...bufRef.current, p]
    if (arr.length > MAX_POINTS) arr.splice(0, arr.length - MAX_POINTS)
    bufRef.current = arr
    setBuf(arr)
  }, [snap])

  const setHidden = (next: Set<string>) => {
    setHiddenState(next)
    try {
      localStorage.setItem(HIDDEN_KEY, JSON.stringify([...next]))
    } catch {
      // best-effort persistence
    }
  }
  const setLines = (next: Set<string>) => {
    setLinesState(next)
    try {
      localStorage.setItem(LINES_KEY, JSON.stringify([...next]))
    } catch {
      // best-effort persistence
    }
  }

  // Normal mode: clicking a chip toggles the sensor's LINE only — the chip
  // stays in the widget (dimmed) so it can be toggled back on.
  const toggleLine = (id: string) => {
    const next = new Set(lines)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setLines(next)
  }

  // Edit mode: clicking a chip (un)selects the sensor in the graph's OWN hide
  // state — unselected sensors are hidden from the graph's normal mode but are
  // untouched in the Temperatures widget.
  const toggleUnselected = (id: string) => {
    const next = new Set(hidden)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setHidden(next)
  }

  const thermals = snap.thermals ?? []
  const colors = useMemo(() => {
    const m = new Map<string, string>()
    thermals.forEach((t, i) => m.set(String(t.id), PALETTE[i % PALETTE.length]))
    return m
  }, [thermals])

  const lastPt = buf[buf.length - 1]

  return (
    <WidgetShell title="Temp graph" icon={<span>📈</span>}>
      {thermals.length === 0 ? (
        <div className="text-sm text-(--text-faint)">No thermal sensors.</div>
      ) : (
        <div className="flex flex-col h-full min-h-0">
          <div className="flex flex-wrap items-center gap-1.5 mb-2 shrink-0">
            <button
              className="px-2 py-0.5 rounded-full text-[10px] border border-(--border) text-(--text-muted) hover:text-(--text) hover:border-(--accent)"
              onClick={() => (edit ? setHidden(new Set()) : setLines(new Set()))}
            >
              All
            </button>
            <button
              className="px-2 py-0.5 rounded-full text-[10px] border border-(--border) text-(--text-muted) hover:text-(--text) hover:border-(--accent)"
              onClick={() => (edit ? setHidden(new Set(thermals.map((t) => String(t.id)))) : setLines(new Set(thermals.map((t) => String(t.id)))))}
            >
              None
            </button>
            {thermals.map((t) => {
              const id = String(t.id)
              const unselected = hidden.has(id) // unselected in edit mode
              const lineOff = lines.has(id) // line toggled off (chip stays)
              const c = colors.get(id) ?? '#888'
              const v = lastPt?.vals[id]
              const label = displayName(cfg, id, t.name)
              // Unselected sensors are hidden from the graph's normal mode; in
              // edit mode they show dimmed so you can restore them.
              if (unselected && !edit) return null
              // Outside edit mode the chip NEVER disappears when clicked: it
              // only toggles the sensor's line (dimmed while off).
              const off = edit ? unselected || lineOff : lineOff
              return (
                <button
                  key={id}
                  onClick={() => (edit ? toggleUnselected(id) : toggleLine(id))}
                  title={
                    edit
                      ? unselected
                        ? 'Restore sensor (show in normal mode)'
                        : 'Unselect sensor (hide from normal mode)'
                      : `${label} — click to ${lineOff ? 'show' : 'hide'} its line`
                  }
                  className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] border transition ${
                    off
                      ? 'border-(--border) text-(--text-faint) opacity-40 hover:opacity-70'
                      : 'border-(--border) bg-(--bg-panel-2) text-(--text-muted) hover:text-(--text)'
                  }`}
                >
                  <span className="inline-block w-2 h-2 rounded-full" style={{ background: c }} />
                  <span className="max-w-[110px] truncate">{label}</span>
                  <span className="mono">{v != null && isFinite(v) ? `${Math.round(v)}°` : '—'}</span>
                </button>
              )
            })}
          </div>
          <div className="flex-1 min-h-[120px]">
            <LineChart
              pts={buf}
              ids={thermals.filter((t) => !hidden.has(String(t.id))).map((t) => String(t.id))}
              hidden={lines}
              colors={colors}
              warn={thresholds?.gpuTempWarn}
              hard={thresholds?.gpuTempHard}
            />
          </div>
        </div>
      )}
    </WidgetShell>
  )
}
