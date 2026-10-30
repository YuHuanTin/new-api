import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useAutoRefresh, useAutoRefreshTick } from '../use-auto-refresh'

afterEach(() => {
  vi.useRealTimers()
  localStorage.clear()
})

describe('useAutoRefreshTick', () => {
  it('runs the action once per interval', () => {
    vi.useFakeTimers()
    const action = vi.fn()
    renderHook(() => useAutoRefreshTick(5_000, action))

    act(() => {
      vi.advanceTimersByTime(5_000)
    })
    expect(action).toHaveBeenCalledTimes(1)

    act(() => {
      vi.advanceTimersByTime(5_000)
    })
    expect(action).toHaveBeenCalledTimes(2)
  })

  it('stays idle while the interval is off', () => {
    vi.useFakeTimers()
    const action = vi.fn()
    renderHook(() => useAutoRefreshTick(0, action))

    act(() => {
      vi.advanceTimersByTime(60_000)
    })
    expect(action).not.toHaveBeenCalled()
  })
})

describe('useAutoRefresh', () => {
  it('keeps one interval for every log page', () => {
    const first = renderHook(() => useAutoRefresh())
    act(() => {
      first.result.current.setInterval(10_000)
    })
    expect(first.result.current.interval).toBe(10_000)
    expect(localStorage.getItem('auto-refresh:interval')).toBe('10000')

    const second = renderHook(() => useAutoRefresh())
    expect(second.result.current.interval).toBe(10_000)
  })
})
