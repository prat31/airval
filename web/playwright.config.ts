import { defineConfig, devices } from '@playwright/test'

/**
 * Golden-path end-to-end tests against a real `airval dev` (SQLite + embedded NATS + built-in mock
 * AI service) started on a temp data dir and a random port by e2e/global-setup.ts.
 *
 *   npm --prefix web run e2e                       # builds bin/airval (make build) first
 *   AIRVAL_E2E_SKIP_BUILD=1 npm --prefix web run e2e   # reuse an existing bin/airval
 *   AIRVAL_E2E_URL=http://127.0.0.1:8080 npm …     # run against a server you started yourself (fresh data dir)
 */
export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.ts',
  fullyParallel: false,
  workers: 1,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never', outputFolder: 'e2e-report' }]] : 'list',
  outputDir: 'e2e-results',
  use: {
    ...devices['Desktop Chrome'],
    // Full Chromium in new headless mode (one browser download serves headed and headless runs).
    channel: 'chromium',
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
})
