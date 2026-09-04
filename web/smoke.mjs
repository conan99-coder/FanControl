// Playwright smoke test: loads the dashboard and asserts the widgets render.
// Run with the mock server running on http://127.0.0.1:8095 (auth disabled).
import { chromium } from '@playwright/test'
import { spawn } from 'node:child_process'

const BASE = 'http://127.0.0.1:8095'

// Ensure the Go mock server is up before the test.
async function waitFor(url, tries = 40) {
  for (let i = 0; i < tries; i++) {
    try {
      const r = await fetch(url)
      if (r.ok) return true
    } catch {}
    await new Promise((r) => setTimeout(r, 250))
  }
  return false
}

const run = async () => {
  const up = await waitFor(BASE + '/api/meta')
  if (!up) {
    console.error('FAIL: mock server not reachable at ' + BASE)
    process.exit(2)
  }

  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } })
  const errors = []
  page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()) })
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message))

  await page.goto(BASE, { waitUntil: 'domcontentloaded' })
  // Wait for the dashboard to render past "Connecting to live data…".
  await page.waitForSelector('.grid-stack-item', { timeout: 15000 }).catch(() => {})

  // Let SSE send a real snapshot so widgets populate.
  await page.waitForTimeout(2500)

  // REPRODUCE the stale-layout bug: seed localStorage with an OLD 8-widget
  // layout (no 'drives'), reload, and confirm the new widget still appears.
  await page.evaluate(() => {
    localStorage.setItem(
      'fc-layout',
      JSON.stringify([
        { id: 'summary', x: 0, y: 0, w: 12, h: 1 },
        { id: 'gpu0', x: 0, y: 1, w: 4, h: 3 },
        { id: 'gpu1', x: 4, y: 1, w: 4, h: 3 },
        { id: 'cpu', x: 8, y: 1, w: 4, h: 3 },
        { id: 'fans', x: 0, y: 4, w: 6, h: 3 },
        { id: 'temps', x: 6, y: 4, w: 6, h: 3 },
        { id: 'disk', x: 0, y: 7, w: 6, h: 2 },
        { id: 'net', x: 6, y: 7, w: 6, h: 2 },
      ])
    )
  })
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.grid-stack-item', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(2000)

  const gridItems = await page.locator('.grid-stack-item').count()
  const summary = await page.locator('text=CPU Load').count()
  const gpuWidget = await page.locator('text=NVIDIA RTX 6000 Pro Blackwell WS').count()
  const drivesWidget = await page.locator('text=Drives (NVMe)').count()
  const hasLogin = await page.locator('text=Sign in').count()

  // Directly read the API the SPA uses, to isolate SSE payload vs render bug.
  const metrics = await page.evaluate(async () => {
    const r = await fetch('/api/metrics')
    return r.json()
  })

  const diskTemps = (metrics.disks || []).map((d) => d.temp ?? 0)
  const driveCount = (metrics.drives || []).length

  const result = {
    gridItems,
    summary,
    gpuWidget,
    drivesWidget,
    hasLogin,
    apiGpuCount: metrics.gpus?.length,
    apiDiskTemp: diskTemps,
    driveCount,
    apiCpuLoad: metrics.cpu?.loadPct,
    consoleErrors: errors,
    bodyTextSnippet: (await page.locator('body').innerText()).slice(0, 200),
  }
  console.log('RESULT', JSON.stringify(result, null, 2))
  await browser.close()

  const ok = gridItems > 0 && summary > 0 && drivesWidget > 0 && !hasLogin && metrics.gpus?.length > 0
  if (!ok) { console.error('FAIL: dashboard did not render correctly (drives widget: ' + drivesWidget + ')'); process.exit(1) }
  console.log('PASS: dashboard rendered with ' + gridItems + ' widget(s), drives widget ' + drivesWidget + ', ' + metrics.gpus?.length + ' GPU(s)')
  process.exit(0)
}

run().catch((e) => { console.error(e); process.exit(1) })



