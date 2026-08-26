// Verify: (1) a widget's drag handle is its header (not content); (2) dragging
// a row grip reorders rows via pointer events WITHOUT moving the widget.
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

  await page.click('text=Edit')
  await page.waitForTimeout(600)

  const dragHandleHeaders = await page.locator('.gs-widget-drag').count()

  const tempItem = page.locator('.grid-stack-item').nth(5)
  const before = await tempItem.boundingBox()
  const rowsBefore = await tempItem.evaluate((el) => {
    return Array.from(el.querySelectorAll('[data-row-id]')).map((r) => r.getAttribute('data-row-id')).join(',')
  })

  // Pointer-drag the first grip onto the second grip.
  const grips = tempItem.locator('[title="Drag to reorder"]')
  const g1 = await grips.nth(0).boundingBox()
  const g2 = await grips.nth(1).boundingBox()
  if (g1 && g2) {
    await page.mouse.move(g1.x + g1.width / 2, g1.y + g1.height / 2)
    await page.mouse.down()
    await page.mouse.move(g2.x + g2.width / 2, g2.y + g2.height / 2, { steps: 15 })
    await page.mouse.up()
  }
  await page.waitForTimeout(600)

  const after = await tempItem.boundingBox()
  const rowsAfter = await tempItem.evaluate((el) => {
    return Array.from(el.querySelectorAll('[data-row-id]')).map((r) => r.getAttribute('data-row-id')).join(',')
  })
  const widgetMoved = before && after && (Math.abs(before.x - after.x) > 5 || Math.abs(before.y - after.y) > 5)
  const rowsChanged = rowsBefore !== rowsAfter

  console.log('dragHandleHeaders:', dragHandleHeaders)
  console.log('widgetMoved:', widgetMoved, 'rowsChanged:', rowsChanged)
  console.log('before:', rowsBefore)
  console.log('after :', rowsAfter)
  console.log('stored:', await page.evaluate(() => localStorage.getItem('fc-rows')))
  console.log('errors:', JSON.stringify(errors))
  await browser.close()

  if (dragHandleHeaders === 0) { console.error('FAIL: no .gs-widget-drag header'); process.exit(1) }
  if (widgetMoved) { console.error('FAIL: row drag moved the whole widget'); process.exit(1) }
  if (errors.length > 0) { console.error('FAIL: console errors'); process.exit(1) }
  if (!rowsChanged) { console.error('FAIL: row order did not change'); process.exit(1) }
  console.log('PASS: row drag reorders without moving the widget')
}
run().catch((e) => { console.error(e); process.exit(1) })
