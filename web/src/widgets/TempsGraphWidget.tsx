import { useEffect, useId, useMemo, useRef, useState, type RefObject } from 'react'
import type { Snapshot } from '../types'
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
const HIDDEN_KEY = 'fc-tempsgraph-hidden'

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

function loadHidden(): Set<string> {
  try {
    const raw = localStorage.getItem(HIDDEN_KEY)
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

// niceTicks returns ~4 round-ish ticks covering [min, max].
function niceTicks(min: number, max: number): number[] {
  const span = max - min || 1
  const step0 = span / 4
  const mag = Math.pow(10, Math.floor(Math.log10(step0)))
  const norm = step0 / mag
  const step = (norm >= 5 ? 5 : norm >= 2 ? 2 : 1) * mag
  const start = Math.ceil(min / step) * step
  const out: number[] = []
  for (let v = start; v <= max + 1e-9; v += step) out.push(v)
  return out
}

// useWidth tracks the rendered width of an element (responsive chart).
function useWidth(): [RefObject<HTMLDivElement>, number] {
  const ref = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(0)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        setW(Math.round(e.contentRect.width))
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, w]
}

function LineChart({
  pts,
  ids,
  hidden,
  colors,
}: {
  pts: Pt[]
  ids: string[]
  hidden: Set<string>
  colors: Map<string, string>
}) {
  const clipId = useId()
  const [ref, width] = useWidth()
  const H = 190
  const M = { l: 42, r: 12, t: 10, b: 20 }
  const W = width > 0 ? width : 640
  const plotW = W - M.l - M.r
  const plotH = H - M.t - M.b

  const visible = ids.filter((id) => !hidden.has(id))

  if (pts.length < 2) {
    return (
      <div ref={ref} className="flex items-center justify-center h-[190px] text-xs text-(--text-faint)">
        Collecting…
      </div>
    )
  }

  const tMax = pts[pts.length - 1].t
  const tMin = Math.max(pts[0].t, tMax - WINDOW_MS)
  const inWin = pts.filter((p) => p.t >= tMin)

  const allVals: number[] = []
  for (const id of visible) {
    for (const p of inWin) {
      const v = p.vals[id]
      if (v != null && isFinite(v)) allVals.push(v)
    }
  }
  if (allVals.length === 0 || visible.length === 0) {
    return (
      <div ref={ref} className="flex items-center justify-center h-[190px] text-xs text-(--text-faint)">
        {visible.length === 0 ? 'All sensors hidden — click a chip to show it.' : 'Collecting…'}
      </div>
    )
  }

  let dmin = Math.min(...allVals)
  let dmax = Math.max(...allVals)
  const pad = Math.max((dmax - dmin) * 0.12, 3)
  dmin -= pad
  dmax += pad
  const ticks = niceTicks(dmin, dmax)
  if (ticks.length > 0) {
    dmin = ticks[0]
    dmax = ticks[ticks.length - 1]
    if (dmax - dmin < 1) dmax = dmin + 1
  }

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
    <div ref={ref} className="w-full">
      <svg width={W} height={H} className="block">
        <defs>
          <clipPath id={clipId}>
            <rect x={M.l} y={M.t} width={plotW} height={plotH} />
          </clipPath>
        </defs>
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

export function TempsGraphWidget({ snap }: { snap: Snapshot }) {
  const bufRef = useRef<Pt[]>([])
  const [buf, setBuf] = useState<Pt[]>([])
  const [hidden, setHiddenState] = useState<Set<string>>(loadHidden)
  const bootRef = useRef(false)

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

  const toggle = (id: string) => {
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
        <>
          <div className="flex flex-wrap items-center gap-1.5 mb-2">
            <button
              className="px-2 py-0.5 rounded-full text-[10px] border border-(--border) text-(--text-muted) hover:text-(--text) hover:border-(--accent)"
              onClick={() => setHidden(new Set())}
            >
              All
            </button>
            <button
              className="px-2 py-0.5 rounded-full text-[10px] border border-(--border) text-(--text-muted) hover:text-(--text) hover:border-(--accent)"
              onClick={() => setHidden(new Set(thermals.map((t) => String(t.id))))}
            >
              None
            </button>
            {thermals.map((t) => {
              const id = String(t.id)
              const on = !hidden.has(id)
              const c = colors.get(id) ?? '#888'
              const v = lastPt?.vals[id]
              return (
                <button
                  key={id}
                  onClick={() => toggle(id)}
                  title={on ? 'Click to hide' : 'Click to show'}
                  className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] border transition ${
                    on
                      ? 'border-(--border) bg-(--bg-panel-2) text-(--text-muted) hover:text-(--text)'
                      : 'border-(--border) text-(--text-faint) opacity-40 hover:opacity-70'
                  }`}
                >
                  <span className="inline-block w-2 h-2 rounded-full" style={{ background: c }} />
                  <span className="max-w-[110px] truncate">{t.name}</span>
                  <span className="mono">{v != null && isFinite(v) ? `${Math.round(v)}°` : '—'}</span>
                </button>
              )
            })}
          </div>
          <LineChart pts={buf} ids={thermals.map((t) => String(t.id))} hidden={hidden} colors={colors} />
        </>
      )}
    </WidgetShell>
  )
}
