import { afterEach, describe, expect, it, vi } from 'vitest'

import { getPresetDateRange } from '../preset-range'

afterEach(() => {
  vi.useRealTimers()
})

describe('getPresetDateRange', () => {
  it('spans today from midnight to the next midnight', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-28T15:30:00'))

    const { start, end } = getPresetDateRange(0)

    expect(start.getTime()).toBe(new Date('2026-10-28T00:00:00').getTime())
    expect(end.getTime()).toBe(new Date('2026-10-29T00:00:00').getTime())
  })

  it('keeps rolling the full window for a day preset', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-28T15:30:00'))

    const { start, end } = getPresetDateRange(7)

    expect(end.getTime() - start.getTime()).toBe(7 * 24 * 60 * 60 * 1000)
  })
})
