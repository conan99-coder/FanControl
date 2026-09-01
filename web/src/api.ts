import type { Snapshot, FanProfile, DMeta } from './types'

export interface Capabilities {
  profiles: boolean
  dutyOverride: boolean
  gpuFanControl: boolean
}

export interface Thresholds {
  gpuTempWarn: number
  gpuTempHard: number
  cpuTempWarn: number
  cpuTempHard: number
  diskUsedWarn: number
}

export interface Status {
  read_only: boolean
  dry_run: boolean
  monitor: boolean
  governor_tripped: boolean
  governor_reason?: string
  capabilities?: Capabilities
  thresholds?: Thresholds
}

// api wires the fetch calls. Auth uses an HttpOnly cookie (set by /api/login),
// so requests need no Authorization header on same-origin.
async function json<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    ...init,
  })
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new ApiError(res.status, text || res.statusText)
  }
  return (await res.json()) as T
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export interface Meta {
  auth_enabled: boolean
  version: string
}

export function getMeta(): Promise<Meta> {
  return json('/api/meta')
}

export function login(username: string, password: string): Promise<{ token: string; user: { name: string; role: string } }> {
  return json('/api/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export function logout(): Promise<{ ok: string }> {
  return json('/api/logout', { method: 'POST' })
}

export function getMetrics(): Promise<Snapshot> {
  return json('/api/metrics')
}

// getHistory returns up to the last ~300 snapshots (time-series as an array).
export function getHistory(): Promise<Snapshot[]> {
  return json('/api/history')
}

export function getStatus(): Promise<Status> {
  return json('/api/status')
}

// setMode toggles Monitor (true, display-only) vs Control (false). Admin-only.
export function setMode(monitor: boolean): Promise<{ monitor: boolean }> {
  return json('/api/mode', { method: 'POST', body: JSON.stringify({ monitor }) })
}

export function getDiscovery(): Promise<DMeta[]> {
  return json('/api/discovery')
}

export async function getProfiles(): Promise<FanProfile[]> {
  const p = await json<FanProfile[] | null>('/api/fan/profiles')
  return Array.isArray(p) ? p : []
}

export async function getActiveProfile(): Promise<{ active: string; mode: string }> {
  const a = await json<{ active?: string; mode?: string } | null>('/api/fan/active')
  return { active: a?.active ?? '', mode: a?.mode ?? '' }
}

// setFanMode switches the global fan mode (Auto|Full|Half). Admin-only.
export function setFanMode(mode: string): Promise<{ ok: string }> {
  return json('/api/fan/mode', { method: 'POST', body: JSON.stringify({ mode }) })
}

export function setFanDuty(fan: number, duty: number): Promise<{ ok: string }> {
  return json('/api/fan/duty', { method: 'POST', body: JSON.stringify({ fan, duty }) })
}

export function setGPUFan(gpu: number, pct: number): Promise<{ ok: string }> {
  return json('/api/fan/gpu', { method: 'POST', body: JSON.stringify({ gpu, pct }) })
}

// streamMetrics opens an SSE connection and invokes cb on each snapshot.
export function streamMetrics(cb: (snap: Snapshot) => void): () => void {
  const es = new EventSource('/api/stream')
  es.onmessage = (ev) => {
    try {
      cb(JSON.parse(ev.data))
    } catch {
      // ignore malformed frames
    }
  }
  return () => es.close()
}
