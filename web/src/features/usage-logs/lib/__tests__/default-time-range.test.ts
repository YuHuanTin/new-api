import { afterEach, describe, expect, it, vi } from 'vitest'

import { getDefaultTimeRange } from '../utils'

afterEach(() => {
  vi.useRealTimers()
})

describe('getDefaultTimeRange', () => {
  it('covers the whole of today, from midnight to the next midnight', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-28T15:30:00'))

    const { start, end } = getDefaultTimeRange()

    expect(start.getTime()).toBe(new Date('2026-10-28T00:00:00').getTime())
    expect(end.getTime()).toBe(new Date('2026-10-29T00:00:00').getTime())
  })
})
