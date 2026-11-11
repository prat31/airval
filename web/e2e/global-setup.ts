/**
 * Builds the binary (unless AIRVAL_E2E_SKIP_BUILD=1) and starts `airval dev` in a fresh temp
 * directory on a random loopback port. The URL is handed to the tests via AIRVAL_E2E_URL; the
 * returned teardown stops the server and removes the data dir.
 */
import { execFileSync, spawn } from 'node:child_process'
import { mkdtempSync, openSync, rmSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
export const binary = process.env.AIRVAL_BIN ?? join(repo, 'bin', 'airval')

function freePort(): Promise<number> {
  return new Promise((ok, fail) => {
    const s = createServer()
    s.once('error', fail)
    s.listen(0, '127.0.0.1', () => {
      const port = (s.address() as { port: number }).port
      s.close(() => ok(port))
    })
  })
}

async function waitHealthy(url: string, ms: number) {
  const deadline = Date.now() + ms
  for (;;) {
    try {
      const r = await fetch(`${url}/readyz`)
      if (r.ok) return
    } catch {
      /* not up yet */
    }
    if (Date.now() > deadline) throw new Error(`airval did not become ready at ${url} within ${ms} ms`)
    await new Promise((r) => setTimeout(r, 200))
  }
}

export default async function globalSetup() {
  if (process.env.AIRVAL_E2E_URL) return undefined
  if (process.env.AIRVAL_E2E_SKIP_BUILD !== '1' && !process.env.AIRVAL_BIN) {
    execFileSync('make', ['build'], { cwd: repo, stdio: 'inherit' })
  }
  const dir = mkdtempSync(join(tmpdir(), 'airval-e2e-'))
  const port = await freePort()
  const url = `http://127.0.0.1:${port}`
  const log = openSync(join(dir, 'airval.log'), 'w')
  const proc = spawn(binary, ['dev', '--listen', `127.0.0.1:${port}`], {
    cwd: dir,
    stdio: ['ignore', log, log],
    env: { ...process.env, AIRVAL_TELEMETRY_LOG_LEVEL: 'warn' },
  })
  proc.on('exit', (code) => {
    if (code && code !== 0) console.error(`airval exited with ${code}; see ${join(dir, 'airval.log')}`)
  })
  await waitHealthy(url, 30_000)
  process.env.AIRVAL_E2E_URL = url
  process.env.AIRVAL_E2E_DIR = dir
  process.env.AIRVAL_E2E_BIN = binary

  return async () => {
    proc.kill('SIGTERM')
    await new Promise((r) => proc.once('exit', r).once('error', r))
    if (process.env.AIRVAL_E2E_KEEP !== '1') rmSync(dir, { recursive: true, force: true })
  }
}
