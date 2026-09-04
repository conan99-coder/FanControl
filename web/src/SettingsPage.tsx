import { useEffect, useState } from 'react'
import type { Settings, SettingsUser, Sources } from './api'
import type { WidgetPrefs } from './rowconfig'
import * as api from './api'

// SettingsPage is the full configuration editor: every config feature is
// presented as a labeled form control (no YAML). Secrets are write-only;
// "restart required" fields are marked. Saves are partial and audit-logged
// server-side.
type Tab = 'general' | 'thresholds' | 'bmc' | 'gpu' | 'vast' | 'docker' | 'auth' | 'widgets'

const TABS: { id: Tab; label: string }[] = [
  { id: 'general', label: 'General' },
  { id: 'thresholds', label: 'Thresholds' },
  { id: 'bmc', label: 'BMC' },
  { id: 'gpu', label: 'GPU' },
  { id: 'vast', label: 'Vast' },
  { id: 'docker', label: 'Docker' },
  { id: 'auth', label: 'Auth' },
  { id: 'widgets', label: 'Widgets' },
]

export function SettingsPage({
  onClose,
  sources,
  widgetPrefs,
  onWidgetPrefs,
}: {
  onClose: () => void
  sources?: Sources
  widgetPrefs?: WidgetPrefs
  onWidgetPrefs?: (p: WidgetPrefs) => void
}) {
  const [tab, setTab] = useState<Tab>('general')
  const [form, setForm] = useState<Settings | null>(null)
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [bmcPass, setBmcPass] = useState('')
  const [vastKey, setVastKey] = useState('')
  const [testResult, setTestResult] = useState('')
  const [restartNeeded, setRestartNeeded] = useState(false)

  useEffect(() => {
    api
      .getSettings()
      .then(setForm)
      .catch((e) => setErr('load settings: ' + (e as Error).message))
  }, [])

  if (!form) {
    return (
      <Overlay onClose={onClose} title="Settings">
        <div className="text-sm text-(--text-faint) p-4">{err || 'Loading settings…'}</div>
      </Overlay>
    )
  }

  const up = (patch: Partial<Settings>) => setForm((f) => (f ? { ...f, ...patch } : f))
  const upT = (patch: Partial<Settings['thresholds']>) => setForm((f) => (f ? { ...f, thresholds: { ...f.thresholds, ...patch } } : f))

  // Fields that need a service restart to take effect.
  const restartFields = new Set(['listen', 'pollInterval', 'provider'])
  const markRestart = (key: string) => restartFields.has(key) && setRestartNeeded(true)

  const save = async () => {
    setBusy(true)
    setErr('')
    setMsg('')
    // Widget toggles are per-device (localStorage) and intentionally excluded.
    const { configPath, bmcHasPassword, vastHasKey, widgets, ...payload } = form
    try {
      const res = await api.updateSettings(payload)
      setForm(res)
      setMsg(restartNeeded ? 'Saved — restart the service to apply the marked fields.' : 'Saved — applied immediately.')
      setRestartNeeded(false)
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const saveSecret = async (kind: 'bmc' | 'vast') => {
    const value = kind === 'bmc' ? bmcPass : vastKey
    if (!value) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await api.saveSecret(kind, value)
      setMsg(`${kind === 'bmc' ? 'BMC password' : 'Vast API key'} saved — applied immediately.`)
      if (kind === 'bmc') setBmcPass('')
      else setVastKey('')
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const runBmcTest = async () => {
    setTestResult('')
    try {
      const r = await api.testBmc({ url: form.bmcUrl, username: form.bmcUsername, password: bmcPass, insecureTls: form.bmcInsecureTls })
      setTestResult(r.ok ? '✓ BMC reachable (Redfish thermal read OK)' : '✗ ' + (r.error ?? 'failed'))
    } catch (e) {
      setTestResult('✗ ' + (e as Error).message)
    }
  }

  const runVastTest = async () => {
    setTestResult('')
    try {
      const r = await api.testVast(vastKey)
      setTestResult(r.ok ? `✓ Vast key works (${r.machines ?? 0} machine(s))` : '✗ ' + (r.error ?? 'failed'))
    } catch (e) {
      setTestResult('✗ ' + (e as Error).message)
    }
  }

  const restart = async () => {
    if (!confirm('Restart the fanctrl service now?')) return
    try {
      await api.restartService()
      setMsg('Restarting… the dashboard will reconnect shortly.')
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const setUser = (i: number, patch: Partial<SettingsUser>) => {
    const users = form.authUsers.map((u, idx) => (idx === i ? { ...u, ...patch } : u))
    up({ authUsers: users })
  }

  return (
    <Overlay onClose={onClose} title="Settings">
      <div className="flex gap-2 flex-wrap mb-4">
        {TABS.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`px-3 py-1 rounded-full text-xs font-semibold border transition ${
              tab === t.id
                ? 'border-(--accent) bg-(--accent) text-(--bg)'
                : 'border-(--border) text-(--text-muted) hover:text-(--text)'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      <div className="space-y-4">
        {tab === 'general' && (
          <>
            <Field label="Listen address" hint="Host:port the dashboard binds. Restart required.">
              <input value={form.listen} onChange={(e) => { up({ listen: e.target.value }); markRestart('listen') }} className={inp} />
            </Field>
            <Field label="Poll interval" hint="How often telemetry is collected. Restart required.">
              <input value={form.pollInterval} onChange={(e) => { up({ pollInterval: e.target.value }); markRestart('pollInterval') }} className={inp} />
            </Field>
            <Field label="Provider" hint="real = host + GPU + BMC; mock = fake data for demo.">
              <select value={form.provider} onChange={(e) => { up({ provider: e.target.value }); markRestart('provider') }} className={inp}>
                <option value="real">real</option>
                <option value="mock">mock</option>
              </select>
            </Field>
            <Toggle label="Dry-run" hint="Collect real data but never write to the BMC/GPU (writes logged only)." value={form.dryRun} onChange={(v) => up({ dryRun: v })} />
            <Toggle label="Read-only" hint="Disable every write endpoint entirely." value={form.readOnly} onChange={(v) => up({ readOnly: v })} />
          </>
        )}

        {tab === 'thresholds' && (
          <>
            <Field label="GPU temp warn (°C)" hint="Yellow warning gate for GPU temperatures.">
              <input type="number" value={form.thresholds.gpuTempWarn} onChange={(e) => upT({ gpuTempWarn: Number(e.target.value) })} className={inp} />
            </Field>
            <Field label="GPU temp hard (°C)" hint="Red danger gate — the safety governor reverts fans to Auto above this.">
              <input type="number" value={form.thresholds.gpuTempHard} onChange={(e) => upT({ gpuTempHard: Number(e.target.value) })} className={inp} />
            </Field>
            <Field label="CPU temp warn (°C)" hint="Yellow warning gate for CPU temperature.">
              <input type="number" value={form.thresholds.cpuTempWarn} onChange={(e) => upT({ cpuTempWarn: Number(e.target.value) })} className={inp} />
            </Field>
            <Field label="CPU temp hard (°C)" hint="Red danger gate for CPU temperature.">
              <input type="number" value={form.thresholds.cpuTempHard} onChange={(e) => upT({ cpuTempHard: Number(e.target.value) })} className={inp} />
            </Field>
            <Field label="Disk used warn (%)" hint="Disk-usage warning gate.">
              <input type="number" value={form.thresholds.diskUsedWarn} onChange={(e) => upT({ diskUsedWarn: Number(e.target.value) })} className={inp} />
            </Field>
            <Field label="Governor cooldown" hint="How long the governor stays in cooldown after a hard breach (e.g. 5m).">
              <input value={form.thresholds.cooldown} onChange={(e) => upT({ cooldown: e.target.value })} className={inp} />
            </Field>
          </>
        )}

        {tab === 'bmc' && (
          <>
            <Field label="BMC URL" hint="https://<bmc-ip> of the Gigabyte MC62-G40 BMC.">
              <input value={form.bmcUrl} onChange={(e) => up({ bmcUrl: e.target.value })} className={inp} />
            </Field>
            <Field label="BMC username">
              <input value={form.bmcUsername} onChange={(e) => up({ bmcUsername: e.target.value })} className={inp} />
            </Field>
            <Field label="BMC password" hint={`Stored in ${form.bmcPasswordPath} (${form.bmcHasPassword ? 'configured' : 'not set'}). Enter a new value to change it — the value is never shown again.`}>
              <div className="flex gap-2">
                <input type="password" value={bmcPass} onChange={(e) => setBmcPass(e.target.value)} placeholder={form.bmcHasPassword ? '•••••••• (set)' : 'not set'} className={inp} />
                <button className={btn} disabled={!bmcPass} onClick={() => saveSecret('bmc')}>Save password</button>
              </div>
            </Field>
            <Toggle label="Accept self-signed TLS" hint="Required unless the BMC has a trusted certificate." value={form.bmcInsecureTls} onChange={(v) => up({ bmcInsecureTls: v })} />
            <Field label="Fan profile">
              <input value={form.bmcProfile} onChange={(e) => up({ bmcProfile: e.target.value })} className={inp} />
            </Field>
            <div className="flex gap-2 items-center">
              <button className={btn} onClick={runBmcTest}>Test BMC connection</button>
              {testResult && <span className="text-xs text-(--text-muted)">{testResult}</span>}
            </div>
          </>
        )}

        {tab === 'gpu' && (
          <>
            <Toggle label="GPU telemetry enabled" hint="Collect GPU data via nvidia-smi." value={form.gpuEnabled} onChange={(v) => up({ gpuEnabled: v })} />
            <Field label="nvidia-smi path">
              <input value={form.gpuQuery} onChange={(e) => up({ gpuQuery: e.target.value })} className={inp} />
            </Field>
            <Field label="GPU query interval">
              <input value={form.gpuQueryInterval} onChange={(e) => up({ gpuQueryInterval: e.target.value })} className={inp} />
            </Field>
          </>
        )}

        {tab === 'vast' && (
          <>
            <Toggle label="Vast provider enabled" hint="Earnings/rates/contracts from `vastai show machines`." value={form.vastEnabled} onChange={(v) => up({ vastEnabled: v })} />
            <Field label="vastai CLI path">
              <input value={form.vastCli} onChange={(e) => up({ vastCli: e.target.value })} className={inp} />
            </Field>
            <Field label="Vast API key" hint={`Stored in ${form.vastApiKeyPath} (${form.vastHasKey ? 'configured' : 'not set'}). Write-only.`}>
              <div className="flex gap-2">
                <input type="password" value={vastKey} onChange={(e) => setVastKey(e.target.value)} placeholder={form.vastHasKey ? '•••••••• (set)' : 'not set'} className={inp} />
                <button className={btn} disabled={!vastKey} onClick={() => saveSecret('vast')}>Save key</button>
              </div>
            </Field>
            <Field label="Vast refresh interval">
              <input value={form.vastInterval} onChange={(e) => up({ vastInterval: e.target.value })} className={inp} />
            </Field>
            <Field label="GPU market filter" hint='Comma-separated GPU name prefixes (begins with, case-insensitive). "RTX PRO 6000" matches RTX PRO 6000 WS and RTX PRO 6000 S. Empty = all GPU types.'>
              <input value={form.vastMarketFilter ?? ''} onChange={(e) => up({ vastMarketFilter: e.target.value })} className={inp} placeholder="RTX 5090, RTX PRO 6000" />
            </Field>
            <div className="flex gap-2 items-center">
              <button className={btn} disabled={!vastKey} onClick={runVastTest}>Test API key</button>
              {testResult && <span className="text-xs text-(--text-muted)">{testResult}</span>}
            </div>
          </>
        )}

        {tab === 'docker' && (
          <>
            <Toggle label="Docker provider enabled" hint="Show the renters' containers (metadata only)." value={form.dockerEnabled} onChange={(v) => up({ dockerEnabled: v })} />
            <Field label="docker CLI path">
              <input value={form.dockerCli} onChange={(e) => up({ dockerCli: e.target.value })} className={inp} />
            </Field>
            <Field label="Docker refresh interval">
              <input value={form.dockerInterval} onChange={(e) => up({ dockerInterval: e.target.value })} className={inp} />
            </Field>
          </>
        )}

        {tab === 'auth' && (
          <>
            <Toggle label="Authentication enabled" hint="When on, the dashboard is read-only until you sign in; when off, everyone is an admin." value={form.authEnabled} onChange={(v) => up({ authEnabled: v })} />
            <Field label="Session TTL">
              <input value={form.authSessionTtl} onChange={(e) => up({ authSessionTtl: e.target.value })} className={inp} />
            </Field>
            <Toggle label="Allow unauthenticated writes" hint="Explicit opt-out: unprotected writes on a non-localhost bind (proxy/firewall must gate access)." value={form.authAllowUnauthenticatedWrites} onChange={(v) => up({ authAllowUnauthenticatedWrites: v })} />
            <div className="label mb-1">Users</div>
            {form.authUsers.map((u, i) => (
              <div key={u.name} className="flex gap-2 items-center flex-wrap">
                <input value={u.name} onChange={(e) => setUser(i, { name: e.target.value })} className={inp + ' w-28'} placeholder="name" />
                <input type="password" value={u.password ?? ''} onChange={(e) => setUser(i, { password: e.target.value })} className={inp + ' w-40'} placeholder={u.hash ? 'new password' : 'password'} />
                <select value={u.role} onChange={(e) => setUser(i, { role: e.target.value })} className={inp + ' w-24'}>
                  <option value="admin">admin</option>
                  <option value="viewer">viewer</option>
                </select>
              </div>
            ))}
          </>
        )}

        {tab === 'widgets' && (
          <div className="space-y-1">
            <div className="text-xs text-(--text-faint) mb-2">
              These toggles are <b>per this device</b> (stored in the browser) — each computer/phone gets its own dashboard.
              Widgets whose data source is disabled are hidden automatically.
            </div>
            <div className="mb-2">
              <button
                className={btn}
                onClick={() => onWidgetPrefs?.({})}
                title="Clear this device's overrides and use the server defaults"
              >
                Reset to server defaults
              </button>
            </div>
            {form.widgets.map((w, i) => {
              const source = sourceFor(w.type)
              const disabled = source != null && sources != null && !sources[source]
              const checked = widgetPrefs && w.type in widgetPrefs ? !!widgetPrefs[w.type] : w.show
              return (
                <div key={w.type + '-' + i} className="flex items-center justify-between rounded-lg bg-(--bg-panel-2) px-3 py-2">
                  <div>
                    <div className="text-xs text-(--text)">{labelFor(w.type)}</div>
                    <div className="text-[10px] text-(--text-faint)">
                      {disabled ? `disabled — source off (${source})` : checked ? 'visible on this device' : 'hidden on this device'}
                    </div>
                  </div>
                  <input
                    type="checkbox"
                    disabled={disabled}
                    checked={checked && !disabled}
                    onChange={(e) => {
                      const prefs = { ...(widgetPrefs ?? {}) }
                      prefs[w.type] = e.target.checked
                      onWidgetPrefs?.(prefs)
                    }}
                  />
                </div>
              )
            })}
          </div>
        )}
      </div>

      <div className="mt-5 flex items-center gap-3 flex-wrap">
        <button className={btnPrimary} disabled={busy} onClick={save}>{busy ? 'Saving…' : 'Save changes'}</button>
        <button className={btn} onClick={restart}>Restart service</button>
        {msg && <span className="text-xs text-(--ok)">{msg}</span>}
        {err && <span className="text-xs text-(--danger)">{err}</span>}
      </div>
      <div className="text-[10px] text-(--text-faint) mt-2">
        Config file: {form.configPath || 'not loaded (writes disabled)'}
      </div>
    </Overlay>
  )
}

function Overlay({ onClose, title, children }: { onClose: () => void; title: string; children: React.ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 bg-black/60 flex items-start justify-center p-4 overflow-y-auto" onClick={onClose}>
      <div className="panel w-full max-w-2xl my-4" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between px-4 py-3 border-b border-(--border) bg-(--bg-panel-2)">
          <span className="label">⚙ {title}</span>
          <button onClick={onClose} className="text-(--text-muted) hover:text-(--text)">✕</button>
        </div>
        <div className="p-4">{children}</div>
      </div>
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <div className="text-xs text-(--text-muted) mb-1">{label}</div>
      {children}
      {hint && <div className="text-[10px] text-(--text-faint) mt-1">{hint}</div>}
    </label>
  )
}

function Toggle({ label, hint, value, onChange }: { label: string; hint?: string; value: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-start justify-between gap-3 py-1 cursor-pointer">
      <div>
        <div className="text-xs text-(--text-muted)">{label}</div>
        {hint && <div className="text-[10px] text-(--text-faint) mt-0.5">{hint}</div>}
      </div>
      <input type="checkbox" checked={value} onChange={(e) => onChange(e.target.checked)} />
    </label>
  )
}

const inp = 'w-full rounded-md border border-(--border) bg-(--bg-panel-2) px-2 py-1.5 text-sm outline-none focus:border-(--accent)'
const btn = 'px-3 py-1.5 rounded-md border border-(--border) text-xs text-(--text-muted) hover:text-(--text) disabled:opacity-40'
const btnPrimary = 'px-3 py-1.5 rounded-md bg-(--accent) text-xs font-semibold text-(--bg) hover:opacity-90 disabled:opacity-50'

function sourceFor(type: string): keyof Sources | null {
  switch (type) {
    case 'gpu':
      return 'gpu'
    case 'temps':
    case 'tempsgraph':
    case 'fans':
    case 'volts':
      return 'bmc'
    case 'vast':
      return 'vast'
    case 'vastmarket':
      return 'vast'
    case 'vastlisting':
      return 'vast'
    case 'maintenance':
      return 'vast'
    case 'docker':
      return 'docker'
    default:
      return null
  }
}

function labelFor(type: string): string {
  const labels: Record<string, string> = {
    summary: 'Summary strip',
    gpu: 'GPU card',
    cpu: 'CPU',
    fans: 'Fans',
    temps: 'Temperatures',
    tempsgraph: 'Temp graph',
    drives: 'Drives (NVMe)',
    disk: 'Disk volumes',
    volts: 'Voltages',
    net: 'Network',
    vast: 'Vast rigs',
    vastmarket: 'GPU market',
    vastlisting: 'Vast listing',
    maintenance: 'Maintenance',
    docker: 'Docker instances',
  }
  return labels[type] ?? type
}
