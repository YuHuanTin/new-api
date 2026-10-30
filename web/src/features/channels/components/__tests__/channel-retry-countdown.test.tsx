import { act, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { ChannelRetryCountdown } from '../channel-retry-countdown'

afterEach(() => {
  vi.useRealTimers()
})

// 鼠标停在提示上时倒计时自己往下走，不用等列表重新拉数据
test('counts down once per second while it stays mounted', () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-29T12:00:00'))
  const disabledAt = Math.floor(Date.now() / 1000) - 60

  render(<ChannelRetryCountdown disabledAt={disabledAt} intervalMinutes={10} />)
  expect(screen.getByText('Next retry: in 9m')).toBeVisible()

  act(() => {
    vi.advanceTimersByTime(60_000)
  })
  expect(screen.getByText('Next retry: in 8m')).toBeVisible()
})

test('renders nothing when the health-check interval is unknown', () => {
  render(
    <ChannelRetryCountdown disabledAt={1_700_000_000} intervalMinutes={0} />
  )
  expect(screen.queryByText(/Next retry/)).not.toBeInTheDocument()
})
