// Row-level dashboard configuration: hide/rename/reorder individual rows inside
// widgets (fans, temps, disk, drives, net, volts). Persisted in localStorage.

export interface RowCfg {
  hidden: Record<string, boolean>
  names: Record<string, string>
  order: string[]
}

export type RowConfigs = Record<string, RowCfg>

const KEY = 'fc-rows'

export function emptyCfg(): RowCfg {
  return { hidden: {}, names: {}, order: [] }
}

export function loadRowConfigs(): RowConfigs {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    return typeof parsed === 'object' && parsed !== null ? parsed : {}
  } catch {
    return {}
  }
}

export function saveRowConfigs(cfgs: RowConfigs) {
  try {
    localStorage.setItem(KEY, JSON.stringify(cfgs))
  } catch {
    // ignore quota/serialization errors — config is best-effort
  }
}

// cfgFor returns the (possibly empty) config for a widget key.
export function cfgFor(cfgs: RowConfigs, widget: string): RowCfg {
  return cfgs[widget] ?? emptyCfg()
}

// applyOrder returns rows ordered by cfg.order (unknown/new rows appended in
// their existing order so new sensors always appear).
export function applyOrder<T extends { id: string }>(rows: T[], order: string[]): T[] {
  if (!order || order.length === 0) return rows
  const byId = new Map(rows.map((r) => [r.id, r]))
  const out: T[] = []
  for (const id of order) {
    const r = byId.get(id)
    if (r) out.push(r)
  }
  for (const r of rows) {
    if (!out.includes(r)) out.push(r)
  }
  return out
}

// visibleRows filters out hidden rows (after ordering).
export function visibleRows<T extends { id: string }>(rows: T[], cfg: RowCfg): T[] {
  return applyOrder(rows, cfg.order).filter((r) => !cfg.hidden[r.id])
}

// displayName returns the custom name if set, else the default label.
export function displayName(cfg: RowCfg, id: string, fallback: string): string {
  const n = cfg.names[id]
  return n && n.trim() ? n : fallback
}

// ---- Per-device widget visibility (localStorage) ----
// Widget on/off preferences are per browser/device (phone, work computer,
// private machine). The server keeps only the capability rule (source off =>
// hidden everywhere); these preferences fall back to the server defaults when
// unset for a widget type.

const WIDGET_KEY = 'fc-widgets'

export type WidgetPrefs = Record<string, boolean>

export function loadWidgetPrefs(): WidgetPrefs {
  try {
    const raw = localStorage.getItem(WIDGET_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    return typeof parsed === 'object' && parsed !== null ? parsed : {}
  } catch {
    return {}
  }
}

export function saveWidgetPrefs(prefs: WidgetPrefs) {
  try {
    localStorage.setItem(WIDGET_KEY, JSON.stringify(prefs))
  } catch {
    // best-effort persistence
  }
}
