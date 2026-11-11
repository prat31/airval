/**
 * The v1 definition of done (plan/001.md §16), end to end against a real `airval dev`:
 * setup → onboarding (target test against /_mock, CSV import, presets, first run) → sample project
 * → 1,000-example run with live progress in < 15 s → failed result in the drawer → comparison
 * against the baseline → CI gate with `airval run --gate --markdown`.
 */
import { expect, test, type Page } from '@playwright/test'
import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const base = () => {
  const url = process.env.AIRVAL_E2E_URL
  if (!url) throw new Error('AIRVAL_E2E_URL is not set (global setup did not start airval)')
  return url
}
const bin = () => process.env.AIRVAL_E2E_BIN ?? resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', 'bin', 'airval')

// Test-only credentials for the throwaway instance started by global-setup.
const owner = { name: 'E2E Owner', email: 'owner@e2e.test', password: 'e2e-password-123', org: 'E2E Org' }

test.describe.configure({ mode: 'serial' })

let page: Page

test.beforeAll(async ({ browser }) => {
  const ctx = await browser.newContext({ baseURL: base() })
  page = await ctx.newPage()
})

test.afterAll(async () => {
  await page?.context().close()
})

test('setup creates the owner and organization', async () => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/setup$/)
  await page.getByLabel('Your name').fill(owner.name)
  await page.getByLabel('Email').fill(owner.email)
  await page.getByLabel('Password').fill(owner.password)
  await page.getByLabel('Organization').fill(owner.org)
  await page.getByRole('button', { name: /create workspace/i }).click()
  await expect(page).toHaveURL(/\/onboarding/)
  await expect(page.getByRole('heading', { name: 'Let’s evaluate your AI service' })).toBeVisible()
})

test('onboarding: target test connection against /_mock, CSV import, presets, first run', async () => {
  await page.getByLabel('Name', { exact: true }).fill('E2E Bot')
  await page.getByRole('button', { name: 'Create project' }).click()
  await expect(page).toHaveURL(/step=1/)

  // Target: the built-in mock, tested live before saving.
  await page.getByRole('button', { name: /No endpoint handy/ }).click()
  await page.getByRole('button', { name: 'Run test' }).click()
  await expect(page.getByText('Connected')).toBeVisible()
  await expect(page.getByText(/The answer is [0-9a-f]{8}\./).first()).toBeVisible()
  await page.getByRole('button', { name: 'Save target' }).click()
  await expect(page).toHaveURL(/step=2/)

  // Dataset: import a CSV with question/answer/tags columns.
  const csv = ['question,answer,tags', ...['What is 2+2?', 'Capital of France?', 'Largest planet?'].map((q, i) => `"${q}",answer ${i},"smoke"`)].join('\n')
  await page.getByRole('button', { name: /Import a file/ }).click()
  const dialog = page.getByRole('dialog', { name: 'Import examples' })
  await expect(dialog).toBeVisible()
  await dialog.locator('input[type=file]').setInputFiles({ name: 'golden.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) })
  await dialog.getByRole('button', { name: 'Preview' }).click()
  await dialog.getByRole('button', { name: 'Import examples' }).click()
  await expect(page).toHaveURL(/step=3/)

  // Evaluators: the CSV has expected answers, so reference-based presets are preselected.
  await expect(page.getByRole('checkbox', { name: 'Exact match' })).toBeChecked()
  await page.getByRole('button', { name: /^Add \d+ evaluators?$/ }).click()
  await expect(page).toHaveURL(/step=4/)

  // First run.
  await page.getByRole('button', { name: 'Run suite' }).click()
  await expect(page).toHaveURL(/\/o\/e2e-org\/p\/e2e-bot\/runs\//)
  // Mock answers never equal "answer N", so the 80% pass-rate gate fails: the run says so clearly.
  await expect(page.getByText('Gate failed').or(page.getByText('Failed', { exact: true })).first()).toBeVisible()
  await expect(page.getByText(/3 results|All 3/).first()).toBeVisible()
})

test('sample project loads in one click with two finished runs', async () => {
  await page.goto('/onboarding?org=e2e-org')
  await page.getByTestId('load-sample').click()
  await expect(page).toHaveURL(/\/p\/sample-support-copilot\/overview/, { timeout: 60_000 })
  await expect(page.getByText('Latest pass rate')).toBeVisible()
  await expect(page.getByText('86.4%').first()).toBeVisible()
  await expect(page.getByText('Pass rate by suite')).toBeVisible()
})

test('a 1,000-example run finishes in under 15 s with live updates', async () => {
  await page.getByRole('button', { name: 'Run suite' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Run suite' })
  await expect(dialog.getByText('Support quality')).toBeVisible()
  const events = page.waitForRequest((r) => /\/v1\/runs\/[^/]+\/events$/.test(new URL(r.url()).pathname))
  const started = Date.now()
  await dialog.getByRole('button', { name: 'Start run' }).click()
  await expect(page).toHaveURL(/\/runs\/[0-9a-f-]+/)
  await events // the run detail subscribed to the SSE stream
  await expect(page.getByText('Passed', { exact: true }).first()).toBeVisible({ timeout: 15_000 })
  const elapsed = Date.now() - started
  expect(elapsed, `run took ${elapsed} ms`).toBeLessThan(15_000)
  await expect(page.getByText('All gates passed')).toBeVisible()
  await expect(page.getByText(/All 1,000/)).toBeVisible()
  // Charts rendered from aggregates.
  await expect(page.getByRole('heading', { name: 'Scorecard' })).toBeVisible()
  await expect(page.getByText('Pass rate by tag')).toBeVisible()
})

test('a failed result opens in the drawer', async () => {
  await page.getByRole('radio', { name: /^Failed \d+/ }).click()
  await expect(page).toHaveURL(/status=failed/)
  const row = page.getByRole('row').filter({ hasText: 'The answer is' }).first()
  await row.click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByText('Expected vs output', { exact: false })).toBeVisible()
  await expect(drawer.getByText('Correct answer')).toBeVisible()
  await expect(page).toHaveURL(/result=/)
  await page.keyboard.press('Escape')
  await expect(drawer).toBeHidden()
})

test('compare against the baseline shows regressions and improvements', async () => {
  await page.getByRole('link', { name: 'Compare to baseline' }).click()
  await expect(page).toHaveURL(/\/compare\?runs=/)
  await expect(page.getByText('Scorecard')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Regressions' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Improvements' })).toBeVisible()
  // The default evaluator is the one that moved, so both lists have entries.
  await expect(page.getByText(/^[1-9]\d* examples$/).first()).toBeVisible()
})

test('CI: airval run --gate exits 0 on a passing suite and prints markdown', async () => {
  test.skip(!existsSync(bin()), `binary not found at ${bin()}`)
  const me = await (await page.request.get('/v1/auth/me')).json()
  const csrf = (await page.context().cookies()).find((c) => c.name === 'airval_csrf')?.value ?? ''
  const res = await page.request.post(`/v1/orgs/${me.orgs[0].id}/api-keys`, {
    headers: { 'X-CSRF-Token': csrf },
    data: { name: 'e2e-ci', role: 'editor' },
  })
  expect(res.ok()).toBeTruthy()
  const { key } = await res.json()
  const cli = (...args: string[]) =>
    spawnSync(bin(), ['run', '--server', base(), '--api-key', key, '--project', 'sample-support-copilot', '--no-progress', ...args], {
      encoding: 'utf8',
      timeout: 60_000,
      cwd: process.env.AIRVAL_E2E_DIR ?? join(process.cwd()),
    })

  const pass = cli('--suite', 'support-quality', '--gate', '--markdown', '--label', 'git_sha=e2e')
  expect(pass.status, pass.stderr).toBe(0)
  expect(pass.stdout).toMatch(/^#### /)
  expect(pass.stdout).toContain('| Gate | Result |')
  expect(pass.stdout).toContain('>= 0.8`')
  expect(pass.stdout).not.toContain('&gt;')

  // The production target fails the accuracy gate: exit code 1.
  const targets = await (await page.request.get(`/v1/projects/${await projectId(page)}/targets`)).json()
  const prod = targets.items.find((t: { name: string }) => t.name.endsWith('prod'))
  const fail = cli('--suite', 'support-quality', '--gate', '--target', prod.id)
  expect(fail.status, fail.stdout + fail.stderr).toBe(1)
})

async function projectId(p: Page): Promise<string> {
  const me = await (await p.request.get('/v1/auth/me')).json()
  const list = await (await p.request.get(`/v1/projects?org_id=${me.orgs[0].id}&slug=sample-support-copilot`)).json()
  return list.items[0].id
}

test('signed out: a deep link redirects to login once, then returns after sign-in', async ({ browser }) => {
  // Regression: the guard used to re-read the location while the redirect committed, nesting
  // `/login?next=…` inside itself until the tab crashed.
  const ctx = await browser.newContext({ baseURL: base() })
  const anon = await ctx.newPage()
  const deep = new URL(page.url()).pathname
  await anon.goto(deep)
  await expect(anon).toHaveURL((u) => u.pathname === '/login' && u.searchParams.get('next') === deep)
  await expect(anon.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  await anon.getByLabel('Email').fill(owner.email)
  await anon.getByLabel('Password').fill(owner.password)
  await anon.getByRole('button', { name: 'Sign in' }).click()
  await expect(anon).toHaveURL((u) => u.pathname === deep)
  await ctx.close()
})
