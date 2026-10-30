import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  formatRetryCountdown,
  getNextChannelTestSeconds,
} from '../channel-test-schedule'

afterEach(() => {
  vi.useRealTimers()
})

describe('getNextChannelTestSeconds', () => {
  it('counts from the last health check plus the interval', () => {
    // 上一次巡检 10:00:00，间隔 10 分钟，现在 10:04:30 → 还剩 5m30s
    expect(
      getNextChannelTestSeconds({
        testTime: 1_000_000,
        disabledAt: 1_000_100,
        intervalMinutes: 10,
        now: 1_000_270,
      })
    ).toBe(330)
  })

  it('counts from the disable time when the channel was never tested', () => {
    expect(
      getNextChannelTestSeconds({
        testTime: 0,
        disabledAt: 2_000_000,
        intervalMinutes: 5,
        now: 2_000_060,
      })
    ).toBe(240)
  })

  it('reports zero once the next round is already due', () => {
    expect(
      getNextChannelTestSeconds({
        testTime: 1_000_000,
        intervalMinutes: 10,
        now: 1_000_900,
      })
    ).toBe(0)
  })

  it('has no countdown when the interval or the timestamps are unknown', () => {
    expect(
      getNextChannelTestSeconds({ testTime: 1_000_000, intervalMinutes: 0 })
    ).toBeNull()
    expect(getNextChannelTestSeconds({ intervalMinutes: 10 })).toBeNull()
  })
})

describe('formatRetryCountdown', () => {
  it('switches units as the remaining time grows', () => {
    expect(formatRetryCountdown(45)).toBe('45s')
    expect(formatRetryCountdown(60)).toBe('1m')
    expect(formatRetryCountdown(200)).toBe('3m20s')
    expect(formatRetryCountdown(3_900)).toBe('1h05m')
  })
})
