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

export interface Sources {
  bmc: boolean
  gpu: boolean
  vast: boolean
  docker: boolean
}

export interface Status {
  read_only: boolean
  dry_run: boolean
  monitor: boolean
  governor_tripped: boolean
  governor_reason?: string
  capabilities?: Capabilities
  thresholds?: Thresholds
  sources?: Sources
  widgets?: { type: string; show: boolean }[]
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

// me restores an existing session (page load) or rejects when logged out.
export function me(): Promise<{ user: string; role: string }> {
  return json('/api/me')
}

export function getMetrics(): Promise<Snapshot> {
  return json('/api/metrics')
}

// getHistory returns up to the last ~300 snapshots (time-series as an array).
export function getHistory(): Promise<Snapshot[]> {
  return json('/api/history')
}

// ---- Settings (admin) ----

export interface SettingsUser {
  name: string
  role: string
  hash: boolean
  password?: string // write-only
}

export interface SettingsThresholds {
  gpuTempWarn: number
  gpuTempHard: number
  cpuTempWarn: number
  cpuTempHard: number
  diskUsedWarn: number
  cooldown: string
}

export interface Settings {
  configPath: string
  listen: string
  pollInterval: string
  provider: string
  dryRun: boolean
  readOnly: boolean
  authEnabled: boolean
  authSessionTtl: string
  authAllowUnauthenticatedWrites: boolean
  authUsers: SettingsUser[]
  authSecretPath: string
  bmcUrl: string
  bmcUsername: string
  bmcPasswordPath: string
  bmcHasPassword: boolean
  bmcInsecureTls: boolean
  bmcProfile: string
  gpuEnabled: boolean
  gpuQuery: string
  gpuQueryInterval: string
  vastEnabled: boolean
  vastCli: string
  vastApiKeyPath: string
  vastHasKey: boolean
  vastInterval: string
  dockerEnabled: boolean
  dockerCli: string
  dockerInterval: string
  thresholds: SettingsThresholds
  widgets: { type: string; show: boolean }[]
}

export type SettingsPatch = Partial<Omit<Settings, 'configPath' | 'bmcHasPassword' | 'vastHasKey' | 'thresholds' | 'widgets'>> & {
  thresholds?: Partial<SettingsThresholds>
  widgets?: { type: string; show: boolean }[]
}

export function getSettings(): Promise<Settings> {
  return json('/api/settings')
}

export function updateSettings(patch: SettingsPatch): Promise<Settings> {
  return json('/api/settings/update', { method: 'PUT', body: JSON.stringify(patch) })
}

export function saveSecret(kind: 'bmc' | 'vast', value: string): Promise<{ ok: string }> {
  return json(`/api/settings/secrets/${kind}`, { method: 'POST', body: JSON.stringify({ value }) })
}

export function testBmc(body: { url: string; username: string; password: string; insecureTls: boolean }): Promise<{ ok: boolean; error?: string }> {
  return json('/api/settings/test/bmc', { method: 'POST', body: JSON.stringify(body) })
}

export function testVast(apiKey: string): Promise<{ ok: boolean; error?: string; machines?: number }> {
  return json('/api/settings/test/vast', { method: 'POST', body: JSON.stringify({ apiKey }) })
}

export function restartService(): Promise<{ ok: string }> {
  return json('/api/settings/restart', { method: 'POST' })
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
