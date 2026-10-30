import { useCallback, useEffect, useRef, useState } from 'react'

/** 可选的自刷新间隔（毫秒），0 表示关闭 */
export const AUTO_REFRESH_INTERVALS = [0, 5_000, 10_000, 60_000] as const

// 所有日志页面共用同一个设置：在「通用日志」里改了，异常管理也跟着变
const STORAGE_KEY = 'auto-refresh:interval'

function readStoredInterval(): number {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === null) {
      return 0
    }
    const value = Number(raw)
    return (AUTO_REFRESH_INTERVALS as readonly number[]).includes(value)
      ? value
      : 0
  } catch {
    return 0
  }
}

function writeStoredInterval(value: number): void {
  try {
    localStorage.setItem(STORAGE_KEY, String(value))
  } catch {
    /* 存储不可用时只影响记住选择，不影响本次会话 */
  }
}

export interface AutoRefreshState {
  interval: number
  setInterval: (interval: number) => void
}

/** 读取并记住日志列表的自刷新间隔，默认关闭 */
export function useAutoRefresh(): AutoRefreshState {
  const [interval, setIntervalState] = useState<number>(readStoredInterval)

  const setInterval = useCallback((next: number) => {
    writeStoredInterval(next)
    setIntervalState(next)
  }, [])

  return { interval, setInterval }
}

/**
 * 按间隔重复执行 action。自动刷新不另起一套请求，而是到点就调用页面自己的
 * 「搜索」按钮，走的还是同一条查询
 */
export function useAutoRefreshTick(interval: number, action: () => void): void {
  const actionRef = useRef(action)

  useEffect(() => {
    actionRef.current = action
  }, [action])

  useEffect(() => {
    if (interval <= 0) {
      return
    }
    const timer = window.setInterval(() => {
      actionRef.current()
    }, interval)
    return () => window.clearInterval(timer)
  }, [interval])
}
