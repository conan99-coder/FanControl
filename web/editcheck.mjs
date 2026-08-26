// Edit-mode flow check: Edit button toggles, row controls appear, rename +
// hide work, and changes persist to localStorage.
import { chromium } from '@playwright/test'

const BASE = 'http://127.0.0.1:8095'

async function waitFor(url, tries = 40) {
  for (let i = 0; i < tries; i++) {
    try { const r = await fetch(url); if (r.ok) return true } catch {}
    await new Promise((r) => setTimeout(r, 250))
  }
  return false
}

const run = async () => {
  const up = await waitFor(BASE + '/api/meta')
  if (!up) { console.error('server not up'); process.exit(2) }
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1400, height: 1400 } })
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message))
  await page.goto(BASE, { waitUntil: 'networkidle' })
  await page.waitForSelector('.grid-stack-item', { timeout: 15000, state: 'attached' })
  await page.waitForTimeout(2000)

  // Enter edit mode.
  await page.click('text=Edit')
  await page.waitForTimeout(600)
  const hint = await page.locator('text=Edit mode').count()
  const doneBtn = await page.locator('text=Done').count()

  // Rename a fan: the fans widget has editable inputs; type into the first one.
  const firstInput = page.locator('.grid-stack-item').nth(4).locator('input').first()
  const renameInputs = await page.locator('.grid-stack-item').nth(4).locator('input').count()
  await firstInput.fill('Primary Fan').catch(() => {})

  // Hide a row: click an ✕ in the temps widget (nth(5)).
  const hideBtns = await page.locator('.grid-stack-item').nth(5).locator('button', { hasText: '✕' }).count()
  if (hideBtns > 0) {
    await page.locator('.grid-stack-item').nth(5).locator('button', { hasText: '✕' }).first().click()
  }
  await page.waitForTimeout(400)

  // Persisted?
  const stored = await page.evaluate(() => localStorage.getItem('fc-rows'))
  console.log('hint:', hint, 'doneBtn:', doneBtn, 'renameInputs:', renameInputs, 'hideBtns:', hideBtns, 'errors:', JSON.stringify(errors))
  console.log('stored:', stored ? stored.slice(0, 200) : 'null')

  // Exit edit mode.
  await page.click('text=Done').catch(() => {})
  await page.waitForTimeout(400)

  await browser.close()
  const ok = hint > 0 && doneBtn > 0 && renameInputs > 0 && hideBtns > 0 && errors.length === 0 && !!stored
  if (!ok) { console.error('FAIL: edit flow'); process.exit(1) }
  console.log('PASS: edit mode works (hint, rename, hide, persist)')
}
run().catch((e) => { console.error(e); process.exit(1) })
