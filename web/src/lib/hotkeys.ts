import { useEffect, useRef, useState } from 'react'

/**
 * Keyboard shortcuts with support for sequences ("g r") and modifiers ("mod+k").
 * "mod" is ⌘ on macOS and Ctrl elsewhere. Shortcuts are ignored while typing in inputs
 * unless the binding includes a modifier.
 */
export type HotkeyMap = Record<string, (e: KeyboardEvent) => void>

export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)
export const modKey = isMac ? '⌘' : 'Ctrl'

export function isTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

function normalize(e: KeyboardEvent): string {
  const parts: string[] = []
  if (e.metaKey || e.ctrlKey) parts.push('mod')
  if (e.altKey) parts.push('alt')
  if (e.shiftKey && e.key.length > 1) parts.push('shift')
  parts.push(e.key.toLowerCase())
  return parts.join('+')
}

const SEQUENCE_TIMEOUT = 900

export function useHotkeys(map: HotkeyMap, enabled = true) {
  const mapRef = useRef(map)
  useEffect(() => {
    mapRef.current = map
  })

  useEffect(() => {
    if (!enabled) return
    let pending: string | null = null
    let timer: ReturnType<typeof setTimeout> | undefined

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing) return
      const combo = normalize(e)
      const hasModifier = combo.includes('mod+') || combo.includes('alt+')
      if (!hasModifier && isTypingTarget(e.target)) return
      const bindings = mapRef.current

      if (pending) {
        const seq = `${pending} ${combo}`
        pending = null
        clearTimeout(timer)
        const handler = bindings[seq]
        if (handler) {
          e.preventDefault()
          handler(e)
          return
        }
      }

      const handler = bindings[combo] ?? (e.key === '?' ? bindings['?'] : undefined)
      if (handler) {
        e.preventDefault()
        handler(e)
        return
      }
      if (!hasModifier && Object.keys(bindings).some((k) => k.startsWith(`${combo} `))) {
        pending = combo
        timer = setTimeout(() => (pending = null), SEQUENCE_TIMEOUT)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      clearTimeout(timer)
    }
  }, [enabled])
}

/** Subscribe to a CSS media query. */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => (typeof window !== 'undefined' ? window.matchMedia(query).matches : false))
  useEffect(() => {
    const mq = window.matchMedia(query)
    const onChange = () => setMatches(mq.matches)
    onChange()
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [query])
  return matches
}
