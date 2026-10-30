import { getRollingDateRange, getStartOfDay } from '@/lib/time'

/**
 * 快速范围对应的起止时间。0 表示「今天」，即当天 0 点到次日 0 点；其余按滚动
 * 天数从当前时刻往前推
 */
export function getPresetDateRange(days: number): { start: Date; end: Date } {
  if (days !== 0) {
    return getRollingDateRange(days)
  }
  const start = getStartOfDay(new Date())
  const end = new Date(start)
  end.setDate(end.getDate() + 1)
  return { start, end }
}
