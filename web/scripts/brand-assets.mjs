// Generates every static brand asset from one geometry (keep in sync with src/components/brand/brand.tsx).
// Usage (from web/): node scripts/brand-assets.mjs
import { chromium } from '@playwright/test'
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const web = join(dirname(fileURLToPath(import.meta.url)), '..')
const root = join(web, '..')
const tmp = mkdtempSync(join(tmpdir(), 'airval-brand-'))
const IRIS = '#5E4BD8'
const INK = '#FFFFFF'

/** The glyph on a 32×32 grid. */
const glyph = (ink) =>
  `<circle cx="14" cy="17.5" r="5.3" fill="none" stroke="${ink}" stroke-width="3"/>` +
  `<path d="M20.8 12.2V23.2" stroke="${ink}" stroke-width="3" stroke-linecap="round"/>` +
  `<circle cx="23.6" cy="8.6" r="2.1" fill="${ink}"/>`

/** Rounded tile + glyph. `inset` shrinks the glyph toward the center (maskable safe zone). */
const markSvg = ({ rx = 8, inset = 0 } = {}) => {
  const s = 1 - inset * 2
  const t = 16 * (1 - s)
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
    `<rect width="32" height="32" rx="${rx}" fill="${IRIS}"/>` +
    `<g transform="translate(${t} ${t}) scale(${s})">${glyph(INK)}</g></svg>`
  )
}

const write = (path, data) => {
  mkdirSync(dirname(path), { recursive: true })
  writeFileSync(path, data)
  console.log('wrote', path.replace(root + '/', ''))
}

// SVGs
write(join(web, 'public/favicon.svg'), markSvg())
write(join(root, 'docs/assets/logo.svg'), markSvg())

const browser = await chromium.launch({ channel: 'chromium' })
const page = await browser.newPage()

const png = async (svg, size, path) => {
  await page.setViewportSize({ width: size, height: size })
  await page.setContent(`<html><body style="margin:0;background:transparent">${svg.replace('<svg ', `<svg width="${size}" height="${size}" `)}</body></html>`)
  write(path, await page.screenshot({ omitBackground: true, clip: { x: 0, y: 0, width: size, height: size } }))
}

// Raster icons. Apple applies its own corner mask, so the touch icon is a full square.
await png(markSvg(), 16, join(tmp, 'favicon-16.png'))
await png(markSvg(), 32, join(tmp, 'favicon-32.png'))
await png(markSvg({ rx: 0 }), 180, join(web, 'public/apple-touch-icon.png'))
await png(markSvg(), 192, join(web, 'public/icon-192.png'))
await png(markSvg(), 512, join(web, 'public/icon-512.png'))
await png(markSvg({ rx: 0, inset: 0.1 }), 512, join(web, 'public/icon-maskable-512.png'))
await png(markSvg(), 256, join(root, 'docs/assets/logo-256.png'))

// favicon.ico (16 + 32 PNG entries) for clients that ignore SVG icons.
const ico = (images) => {
  const header = Buffer.alloc(6 + 16 * images.length)
  header.writeUInt16LE(0, 0)
  header.writeUInt16LE(1, 2)
  header.writeUInt16LE(images.length, 4)
  let offset = header.length
  images.forEach(({ size, data }, i) => {
    const e = 6 + 16 * i
    header.writeUInt8(size, e)
    header.writeUInt8(size, e + 1)
    header.writeUInt16LE(1, e + 4)
    header.writeUInt16LE(32, e + 6)
    header.writeUInt32LE(data.length, e + 8)
    header.writeUInt32LE(offset, e + 12)
    offset += data.length
  })
  return Buffer.concat([header, ...images.map((i) => i.data)])
}
write(
  join(web, 'public/favicon.ico'),
  ico([16, 32].map((size) => ({ size, data: readFileSync(join(tmp, `favicon-${size}.png`)) }))),
)

// Social preview (GitHub "Social preview", link unfurls): 1280×640.
const inter = readFileSync(join(web, 'node_modules/@fontsource-variable/inter/files/inter-latin-wght-normal.woff2')).toString('base64')
await page.setViewportSize({ width: 1280, height: 640 })
await page.setContent(`<html><head><style>
@font-face{font-family:Inter;src:url(data:font/woff2;base64,${inter}) format('woff2');font-weight:100 900}
body{margin:0;width:1280px;height:640px;font-family:Inter;background:#F7F8FA;display:flex;align-items:center;justify-content:center}
.card{display:flex;flex-direction:column;align-items:center;gap:28px}
.lock{display:flex;align-items:center;gap:28px}
.name{font-size:120px;font-weight:600;letter-spacing:-0.035em;color:#0F1218;line-height:1}
.tag{font-size:34px;font-weight:450;color:#4A5160;letter-spacing:-0.01em}
.foot{position:absolute;bottom:40px;font-size:22px;color:#7B8392}
.bar{position:absolute;top:0;left:0;right:0;height:10px;background:${IRIS}}
</style></head><body><div class="bar"></div><div class="card">
<div class="lock">${markSvg().replace('<svg ', '<svg width="132" height="132" ')}<span class="name">airval</span></div>
<div class="tag">Self-hosted evaluation for AI services</div></div>
<div class="foot">Datasets · Evaluators · Gates · Runs · CI</div></body></html>`)
await page.evaluate(() => document.fonts.ready)
write(join(root, 'docs/assets/social-preview.png'), await page.screenshot())

// Wordmark lockup for docs (PNG so the font renders the same everywhere).
for (const [theme, fg, file] of [['light', '#0F1218', 'logo-wordmark.png'], ['dark', '#ECEEF2', 'logo-wordmark-dark.png']]) {
  await page.setViewportSize({ width: 560, height: 160 })
  await page.setContent(`<html><head><style>
@font-face{font-family:Inter;src:url(data:font/woff2;base64,${inter}) format('woff2');font-weight:100 900}
body{margin:0;background:transparent;font-family:Inter}
.lock{display:inline-flex;align-items:center;gap:22px;padding:16px}
.name{font-size:92px;font-weight:600;letter-spacing:-0.035em;color:${fg};line-height:1}
</style></head><body><div class="lock" id="l">${markSvg().replace('<svg ', '<svg width="104" height="104" ')}<span class="name">airval</span></div></body></html>`)
  await page.evaluate(() => document.fonts.ready)
  write(join(root, `docs/assets/${file}`), await page.locator('#l').screenshot({ omitBackground: true }))
  void theme
}

await browser.close()
rmSync(tmp, { recursive: true, force: true })
