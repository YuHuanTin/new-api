import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { formatRetryCountdown, getNextChannelTestSeconds } from '../lib'

/**
 * 悬停提示里的「下次重试」倒计时。挂载后每秒重算一次，所以鼠标停在那里就能
 * 看到数字往下走，不用等列表重新拉数据
 */
export function ChannelRetryCountdown(props: {
  testTime?: number
  disabledAt?: number
  intervalMinutes: number
}) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => Date.now() / 1000)

  useEffect(() => {
    const timer = window.setInterval(() => {
      setNow(Date.now() / 1000)
    }, 1000)
    return () => window.clearInterval(timer)
  }, [])

  const seconds = getNextChannelTestSeconds({
    testTime: props.testTime,
    disabledAt: props.disabledAt,
    intervalMinutes: props.intervalMinutes,
    now,
  })
  if (seconds === null) {
    return null
  }
  return (
    <div>
      {t('Next retry: in {{time}}', { time: formatRetryCountdown(seconds) })}
    </div>
  )
}
