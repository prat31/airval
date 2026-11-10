import { describe, expect, it } from 'vitest'
import { withAlpha } from './theme'
import { binValues, formatValue, quantile } from './util'

describe('chart utils', () => {
  it('bins values with max in the last bin', () => {
    const bins = binValues([0, 0.1, 0.5, 0.99, 1], 4, 0, 1)
    expect(bins.map((b) => b.count)).toEqual([2, 0, 1, 2])
  })

  it('interpolates quantiles', () => {
    expect(quantile([1, 2, 3, 4], 0.5)).toBe(2.5)
    expect(quantile([10], 0.99)).toBe(10)
  })

  it('formats percent and ms', () => {
    expect(formatValue(0.8, 'percent')).toBe('80%')
    expect(formatValue(0.825, 'percent')).toBe('82.5%')
    expect(formatValue(1500, 'ms')).toBe('1.5 s')
  })

  it('applies alpha to token colors for canvas', () => {
    expect(withAlpha('#6250d6', 0.5)).toBe('rgba(98, 80, 214, 0.5)')
    expect(withAlpha('rgb(143 127 239 / 0.14)', 0.2)).toBe('rgba(143, 127, 239, 0.2)')
  })
})
