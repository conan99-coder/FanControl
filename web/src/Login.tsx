import { useState } from 'react'
import * as api from './api'

export function Login({ onLogin, onCancel }: { onLogin: (role: string, name: string) => void; onCancel?: () => void }) {
  const [user, setUser] = useState('')
  const [pass, setPass] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr('')
    try {
      const res = await api.login(user, pass)
      onLogin(res.user.role, res.user.name)
    } catch (er) {
      setErr((er as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="panel w-full max-w-sm p-6">
        <div className="flex items-center gap-2 mb-1">
          <span className="text-2xl">🌡️</span>
          <h1 className="text-xl font-semibold">FanControl</h1>
        </div>
        <p className="text-sm text-(--text-muted) mb-5">Rig observability &amp; fan control</p>
        <form onSubmit={submit} className="space-y-3">
          <label className="block text-sm">
            <span className="text-(--text-muted)">Username</span>
            <input
              value={user}
              onChange={(e) => setUser(e.target.value)}
              autoComplete="username"
              className="mt-1 w-full rounded-md border border-(--border) bg-(--bg-panel-2) px-3 py-2 text-sm outline-none focus:border-(--accent)"
            />
          </label>
          <label className="block text-sm">
            <span className="text-(--text-muted)">Password</span>
            <input
              type="password"
              value={pass}
              onChange={(e) => setPass(e.target.value)}
              autoComplete="current-password"
              className="mt-1 w-full rounded-md border border-(--border) bg-(--bg-panel-2) px-3 py-2 text-sm outline-none focus:border-(--accent)"
            />
          </label>
          {err && <div className="text-xs text-(--danger)">{err}</div>}
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded-md bg-(--accent) px-3 py-2 text-sm font-semibold text-(--bg) hover:opacity-90 disabled:opacity-50"
          >
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
          {onCancel && (
            <button
              type="button"
              onClick={onCancel}
              className="w-full rounded-md border border-(--border) px-3 py-2 text-sm text-(--text-muted) hover:text-(--text)"
            >
              ← Back to dashboard
            </button>
          )}
        </form>
      </div>
    </div>
  )
}
