/**
 * DEV-ONLY fixture mode for building run screens without an engine.
 *
 * Enable with `?fixtures=1` (remembered for the tab) or `VITE_FIXTURES=1`; disable with `?fixtures=0`.
 * main.tsx imports this only behind `import.meta.env.DEV`, so production bundles never contain it.
 */
const KEY = 'airval.fixtures'

export async function maybeInstallFixtures() {
  let on = import.meta.env.VITE_FIXTURES === '1'
  try {
    const flag = new URLSearchParams(location.search).get('fixtures')
    if (flag === '1') sessionStorage.setItem(KEY, '1')
    if (flag === '0') sessionStorage.removeItem(KEY)
    on = on || sessionStorage.getItem(KEY) === '1'
  } catch {
    /* storage unavailable */
  }
  if (!on) return
  const { install } = await import('./install')
  install()
}
