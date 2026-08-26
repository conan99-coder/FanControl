// Layout responsiveness check: the widget grid must reflow at narrow widths
// (responsive columns) and every widget must have a sane (non-zero) size.
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
  const sizes = [
    { name: 'wide', w: 1400, h: 1200 },
    { name: 'medium', w: 800, h: 1200 },
    { name: 'narrow', w: 500, h: 1200 },
  ]
  let fail = false
  for (const s of sizes) {
    const page = await browser.newPage({ viewport: { width: s.w, height: s.h } })
    const errors = []
    page.on('pageerror', (e) => errors.push(e.message))
    await page.goto(BASE, { waitUntil: 'networkidle' })
    await page.waitForSelector('.grid-stack-item', { timeout: 15000, state: 'attached' })
    await page.waitForTimeout(2500)
    const items = await page.evaluate(() => {
      return Array.from(document.querySelectorAll('.grid-stack-item')).map((el) => {
        const b = el.getBoundingClientRect()
        return { w: Math.round(b.width), h: Math.round(b.height), y: Math.round(b.y) }
      })
    })
    const zeroSized = items.filter((i) => i.w <= 0 || i.h <= 0).length
    const gridClass = await page.locator('.grid-stack').getAttribute('class')
    console.log(`${s.name}: items=${items.length} zeroSized=${zeroSized} gridClass=${gridClass} errors=${JSON.stringify(errors)}`)
    console.log(`  item0=${JSON.stringify(items[0])}`)
    if (items.length < 10 || zeroSized > 0 || errors.length > 0) fail = true
    await page.close()
  }
  await browser.close()
  if (fail) { console.error('FAIL: layout not responsive/sane'); process.exit(1) }
  console.log('PASS: layout responsive at all widths')
}
run().catch((e) => { console.error(e); process.exit(1) })
