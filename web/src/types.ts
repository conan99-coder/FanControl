// Type definitions mirroring the Go metrics JSON model.
// These must stay in sync with internal/metrics/types.go and discovery.go.

export interface Scalar {
  name: string
  value: number
  unit: string
  kind: number
  min: number
  max: number
}

export interface GPU {
  index: number
  name: string
  temp: number
  util: number
  power: number
  powerLimit: number
  fanPct: number
  fanControl: boolean
  vramUsed: number
  vramTotal: number
  memoryUtil: number
  maxTemp: number
}

export interface Disk {
  mount: string
  device: string
  fsType: string
  totalBytes: number
  freeBytes: number
  readRate: number
  writeRate: number
}

export interface Drive {
  device: string
  model: string
  serial: string
  firmware?: string
  sizeBytes: number
  temp: number
}

export interface Net {
  interface: string
  rxRate: number
  txRate: number
  up: boolean
}

export interface Fan {
  id: number
  name: string
  rpm: number
  duty: number
  maxRpm: number
  autoDuty: number
}

export interface Thermal {
  id: number
  name: string
  temp: number
  max: number
  min: number
}

export interface CPU {
  model: string
  cores: number
  threads: number
  loadPct: number
  uptime: number
  memTotal: number
  memUsed: number
  memAvail: number
  cpuTemp: number
  cpuTempMax: number
  perCoreLoad: number[]
}

export interface VastRig {
  id: number
  hostname: string
  gpuName: string
  numGpus: number
  listedGpuCost: number
  earnHour: number
  earnDay: number
  rentalsRunning: number
  clientEndDate: number
  endDate: number
  verification: string
  reliability: number
  geolocation: string
}

export interface Snapshot {
  time: string
  cpu: Partial<CPU>
  gpus: GPU[]
  disks: Disk[]
  drives: Drive[]
  nets: Net[]
  fans: Fan[]
  thermals: Thermal[]
  extra: Scalar[]
  vastRigs?: VastRig[]
}

export interface Policy {
  fanSensors: number[]
  duty: number[]
  ref: number[]
  sensor: number[]
  initDuty: number
  policyType: number
}

export interface FanProfile {
  name: string
  policies: Policy[]
  mode?: string
}

// discovery
export interface DMeta {
  source: string
  thermals: { id?: number; name: string; hmon?: string }[]
  fans: { id?: number; name: string }[]
  gpus: { index: number; name: string; fan_control: boolean }[]
  disks: string[]
  nets: string[]
  cpu: { model: string; cores: number; threads: number }
  meta?: Record<string, string>
}

export function formatBytes(n: number): string {
  if (!isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  return (n / Math.pow(1024, i)).toFixed(1) + ' ' + units[i]
}

export function formatRate(n: number): string {
  return formatBytes(n) + '/s'
}

export function fmtPct(n: number): string {
  return (isFinite(n) ? n : 0).toFixed(0) + '%'
}

export function fmtUSD(n: number): string {
  return '$' + (isFinite(n) ? n : 0).toFixed(2)
}

export function fmtDate(unixSec: number): string {
  if (!unixSec || !isFinite(unixSec)) return '—'
  const d = new Date(unixSec * 1000)
  return isNaN(d.getTime()) ? '—' : d.toLocaleDateString()
}

// fmtDateTime formats a unix-seconds timestamp as "YYYY-MM-DD HH:mm" (24h).
export function fmtDateTime(unixSec: number): string {
  if (!unixSec || !isFinite(unixSec)) return '—'
  const d = new Date(unixSec * 1000)
  if (isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
