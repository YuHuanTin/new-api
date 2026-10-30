import type { Table } from '@tanstack/react-table'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { AutoRefreshButton } from '@/components/auto-refresh-button'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'
import { getDefaultTimeRange } from '@/features/usage-logs/lib/utils'
import type { AutoRefreshState } from '@/hooks/use-auto-refresh'

import type { ExceptionLog } from '../types'

export interface ExceptionLogsFilters {
  username: string
  errorCode: string
  startTime: Date
  endTime: Date
}

/**
 * 异常管理的筛选栏，和「通用日志」用同一套工具栏：筛选条件先填，点「搜索」
 * 才生效，重置回到全部失败请求
 */
export function ExceptionLogsFilterBar(props: {
  table: Table<ExceptionLog>
  applied: ExceptionLogsFilters
  autoRefresh: AutoRefreshState
  searchLoading: boolean
  onApply: (filters: ExceptionLogsFilters) => void
  onReset: () => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<ExceptionLogsFilters>(props.applied)

  // 地址栏变了（浏览器后退、重置）时把草稿同步回来
  useEffect(() => {
    setDraft(props.applied)
  }, [props.applied])

  // 草稿里有内容，或者地址栏里已经有生效的筛选，都算「可以重置」
  const hasActiveFilters =
    draft.username.trim() !== '' ||
    draft.errorCode.trim() !== '' ||
    props.applied.username !== '' ||
    props.applied.errorCode !== ''

  return (
    <LogsFilterToolbar
      table={props.table}
      compactMobile
      actionStart={
        <AutoRefreshButton
          interval={props.autoRefresh.interval}
          onIntervalChange={props.autoRefresh.setInterval}
        />
      }
      hasActiveFilters={hasActiveFilters}
      searchLoading={props.searchLoading}
      onSearch={() => props.onApply(draft)}
      onReset={() => {
        const { start, end } = getDefaultTimeRange()
        setDraft({
          username: '',
          errorCode: '',
          startTime: start,
          endTime: end,
        })
        props.onReset()
      }}
      primaryFilters={
        <>
          <LogsFilterField wide>
            <CompactDateTimeRangePicker
              start={draft.startTime}
              end={draft.endTime}
              onChange={({ start, end }) =>
                setDraft((previous) => ({
                  ...previous,
                  startTime: start,
                  endTime: end,
                }))
              }
            />
          </LogsFilterField>
          <LogsFilterField>
            <LogsFilterInput
              placeholder={t('Filter by username...')}
              value={draft.username}
              onChange={(event) =>
                setDraft((previous) => ({
                  ...previous,
                  username: event.target.value,
                }))
              }
              onKeyDown={(event) => {
                if (event.key === 'Enter') props.onApply(draft)
              }}
            />
          </LogsFilterField>
          <LogsFilterField>
            <LogsFilterInput
              placeholder={t('Filter by error code...')}
              value={draft.errorCode}
              onChange={(event) =>
                setDraft((previous) => ({
                  ...previous,
                  errorCode: event.target.value,
                }))
              }
              onKeyDown={(event) => {
                if (event.key === 'Enter') props.onApply(draft)
              }}
            />
          </LogsFilterField>
        </>
      }
    />
  )
}
