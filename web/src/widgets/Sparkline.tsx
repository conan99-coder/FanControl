// Sparkline renders a simple SVG trend line from a numeric series.
export function Sparkline({ points, color, width = 120, height = 28 }: { points: (number | null)[]; color: string; width?: number; height?: number }) {
  const vals = points.filter((p): p is number => p !== null && isFinite(p))
  if (vals.length < 2) return <div className="text-[10px] text-(--text-faint)">collecting…</div>
  const min = Math.min(...vals)
  const max = Math.max(...vals)
  const range = max - min || 1
  const step = width / (vals.length - 1)
  const path = vals
    .map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)},${(height - ((v - min) / range) * (height - 4) - 2).toFixed(1)}`)
    .join(' ')
  return (
    <svg width={width} height={height} className="overflow-visible">
      <path d={path} fill="none" stroke={color} strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={width} cy={height - ((vals[vals.length - 1] - min) / range) * (height - 4) - 2} r={2} fill={color} />
    </svg>
  )
}
