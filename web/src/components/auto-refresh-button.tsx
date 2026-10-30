import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { AUTO_REFRESH_INTERVALS } from '@/hooks/use-auto-refresh'
import { cn } from '@/lib/utils'

/** 把毫秒间隔显示成 5s / 10s / 1m */
function formatInterval(t: (key: string) => string, interval: number): string {
  if (interval <= 0) {
    return t('Off')
  }
  if (interval < 60_000) {
    return t(`${interval / 1000}s`)
  }
  return t(`${interval / 60_000}m`)
}

/**
 * 日志列表的自动刷新按钮。间隔保存在浏览器里，所有日志页面共用同一个值
 */
export function AutoRefreshButton(props: {
  interval: number
  onIntervalChange: (interval: number) => void
}) {
  const { t } = useTranslation()
  const active = props.interval > 0

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={
          <Button
            variant='outline'
            className={cn('shrink-0', active && 'text-primary')}
            aria-label={t('Auto refresh')}
          />
        }
      >
        <RefreshCw className={cn('size-4', active && 'animate-spin')} />
        <span className='hidden sm:inline'>
          {t('Auto refresh')}
          {active ? ` · ${formatInterval(t, props.interval)}` : ''}
        </span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end' className='w-[150px]'>
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t('Auto refresh')}</DropdownMenuLabel>
          {AUTO_REFRESH_INTERVALS.map((interval) => (
            <DropdownMenuCheckboxItem
              key={interval}
              checked={props.interval === interval}
              onCheckedChange={() => props.onIntervalChange(interval)}
            >
              {formatInterval(t, interval)}
            </DropdownMenuCheckboxItem>
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
