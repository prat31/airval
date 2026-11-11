// Captures the README screenshots from a seeded throwaway instance.
// Run via scripts/readme-media/build.sh (it seeds the instance first); usage: node scripts/readme-capture.mjs <base-url> <out-dir>
import { chromium } from '@playwright/test'

const [base, outDir] = process.argv.slice(2)
const seed = { email: 'demo@example.test', password: 'readme-demo-1234' }
const P = '/o/acme/p/sample-support-copilot'

const browser = await chromium.launch({ channel: 'chromium' })
const shoot = async (scheme, frames) => {
  const ctx = await browser.newContext({ baseURL: base, viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, colorScheme: scheme })
  const page = await ctx.newPage()
  await page.goto('/login')
  await page.getByLabel('Email').fill(seed.email)
  await page.getByLabel('Password').fill(seed.password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.waitForURL(/\/p\//)

  const ids = await page.evaluate(async () => {
    const j = (u) => fetch(u).then((r) => r.json())
    const me = await j('/v1/auth/me')
    const proj = (await j(`/v1/projects?org_id=${me.orgs[0].id}`)).items[0]
    const runs = (await j(`/v1/runs?project_id=${proj.id}&limit=50`)).items
    const targets = (await j(`/v1/projects/${proj.id}/targets`)).items
    const cand = targets.find((t) => /candidate/.test(t.name))
    const prod = targets.find((t) => /prod/.test(t.name))
    const latestCand = runs.find((r) => r.target_id === cand.id && r.status === 'succeeded')
    const latestProd = runs.find((r) => r.target_id === prod.id)
    const failed = (await j(`/v1/runs/${latestCand.id}/results?status=failed&limit=5`)).items[0]
    const datasets = (await j(`/v1/projects/${proj.id}/datasets`)).items
    const suites = (await j(`/v1/projects/${proj.id}/suites`)).items
    return { cand: latestCand.id, prod: latestProd.id, failed: failed?.id, dataset: datasets.find((d) => /support/i.test(d.name))?.id ?? datasets[0].id, suite: suites[0].id }
  })

  for (const [name, path, after] of frames(ids)) {
    await page.goto(path)
    await page.waitForLoadState('networkidle').catch(() => {})
    await page.waitForTimeout(900) // let charts finish their entry animation
    if (after) await after(page)
    await page.screenshot({ path: `${outDir}/${name}.png` })
    console.log('captured', name)
  }
  await ctx.close()
}

const scrollMain = (y) => async (page) => {
  await page.locator('main').evaluate((el, top) => el.scrollTo({ top }), y)
  await page.waitForTimeout(900)
}

await shoot('light', (ids) => [
  ['01-overview', `${P}/overview`],
  ['02-runs', `${P}/runs`],
  ['03-run-detail', `${P}/runs/${ids.cand}`],
  ['04-run-charts', `${P}/runs/${ids.cand}`, scrollMain(600)],
  ['05-result-drawer', `${P}/runs/${ids.cand}?result=${ids.failed}&tab=output`],
  ['06-compare', `${P}/compare?runs=${ids.prod},${ids.cand}`],
  ['07-suite', `${P}/suites/${ids.suite}`],
  ['08-dataset', `${P}/datasets/${ids.dataset}`],
  ['09-evaluators', `${P}/evaluators`],
])
await shoot('dark', (ids) => [['10-run-detail-dark', `${P}/runs/${ids.cand}`]])
await browser.close()
