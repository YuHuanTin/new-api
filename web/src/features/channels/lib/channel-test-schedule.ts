/** 巡检间隔（分钟）与渠道最近一次巡检、封禁时间，算出距离下次巡检还有多久 */

const SECONDS_PER_MINUTE = 60

/**
 * 下一次渠道巡检距离现在还有多少秒
 *
 * 巡检是按固定间隔跑的系统任务，所以下一次巡检约等于「上一次巡检 + 间隔」；
 * 还没巡检过的渠道（`test_time` 为 0）从封禁时刻起算。已经过期就返回 0，
 * 表示随时会重试。间隔未知或巡检关闭时返回 null
 */
export function getNextChannelTestSeconds(input: {
  testTime?: number
  disabledAt?: number
  intervalMinutes: number
  now?: number
}): number | null {
  const intervalSeconds = input.intervalMinutes * SECONDS_PER_MINUTE
  if (!(intervalSeconds > 0)) {
    return null
  }
  const base = input.testTime || input.disabledAt
  if (!base) {
    return null
  }
  const now = input.now ?? Date.now() / 1000
  return Math.max(0, Math.round(base + intervalSeconds - now))
}

/** 把剩余秒数写成 `45s` / `3m20s` / `1h05m` */
export function formatRetryCountdown(seconds: number): string {
  if (seconds < SECONDS_PER_MINUTE) {
    return `${seconds}s`
  }
  const minutes = Math.floor(seconds / SECONDS_PER_MINUTE)
  if (minutes < SECONDS_PER_MINUTE) {
    const rest = seconds % SECONDS_PER_MINUTE
    return rest === 0 ? `${minutes}m` : `${minutes}m${rest}s`
  }
  const hours = Math.floor(minutes / SECONDS_PER_MINUTE)
  const restMinutes = minutes % SECONDS_PER_MINUTE
  return `${hours}h${String(restMinutes).padStart(2, '0')}m`
}
