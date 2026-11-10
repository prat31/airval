import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useHotkeys, type HotkeyMap } from './hotkeys'

function Harness({ map }: { map: HotkeyMap }) {
  useHotkeys(map)
  return <input aria-label="field" />
}

describe('useHotkeys', () => {
  it('handles sequences like "g r"', () => {
    const go = vi.fn()
    render(<Harness map={{ 'g r': go }} />)
    fireEvent.keyDown(window, { key: 'g' })
    fireEvent.keyDown(window, { key: 'r' })
    expect(go).toHaveBeenCalledOnce()
  })

  it('handles mod combos and ignores plain keys while typing', () => {
    const palette = vi.fn()
    const help = vi.fn()
    const { getByLabelText } = render(<Harness map={{ 'mod+k': palette, '?': help }} />)
    const input = getByLabelText('field')
    fireEvent.keyDown(input, { key: '?' })
    expect(help).not.toHaveBeenCalled()
    fireEvent.keyDown(input, { key: 'k', metaKey: true })
    expect(palette).toHaveBeenCalledOnce()
  })
})
