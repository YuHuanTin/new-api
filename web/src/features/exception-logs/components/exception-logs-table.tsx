import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { getDefaultTimeRange } from '@/features/usage-logs/lib/utils'
import { useAutoRefresh, useAutoRefreshTick } from '@/hooks/use-auto-refresh'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { getExceptionLogs } from '../api'
import type { ExceptionLog } from '../types'
import { ExceptionDetailDialog } from './dialogs/exception-detail-dialog'
import { ExceptionClearCapture } from './exception-clear-capture'
import { useExceptionLogsColumns } from './exception-logs-columns'
import {
  ExceptionLogsFilterBar,
  type ExceptionLogsFilters,
} from './exception-logs-filter-bar'

const route = getRouteApi('/_authenticated/exception-logs/')

export function ExceptionLogsTable() {
  const { t } = useTranslation()
  const [detailLog, setDetailLog] = useState<ExceptionLog | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const search = route.useSearch()
  const navigate = route.useNavigate()
  // 异常请求也默认只看今天，避免 failed_only 的 LIKE 条件全表扫描
  const defaultRange = useMemo(() => getDefaultTimeRange(), [])
  const range = useMemo(
    () => ({
      startTime: search.startTime
        ? new Date(search.startTime)
        : defaultRange.start,
      endTime: search.endTime ? new Date(search.endTime) : defaultRange.end,
    }),
    [search.startTime, search.endTime, defaultRange]
  )

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: { defaultPage: 1, defaultPageSize: 20 },
      globalFilter: { enabled: false },
      columnFilters: [],
    })

  // 筛选条件直接以地址栏为准：异常页的「搜索」一次性写入用户名、错误码和时间段
  const username = typeof search.filter === 'string' ? search.filter : ''
  const errorCode =
    typeof search.error_code === 'string' ? search.error_code : ''
  const columnFilters = useMemo(
    () => (errorCode ? [{ id: 'error_code', value: errorCode }] : []),
    [errorCode]
  )

  const openDetails = useCallback((log: ExceptionLog) => {
    setDetailLog(log)
    setDetailOpen(true)
  }, [])

  const columns = useExceptionLogsColumns(openDetails)
  const autoRefresh = useAutoRefresh()

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: [
      'exception-logs',
      pagination.pageIndex + 1,
      pagination.pageSize,
      username,
      errorCode,
      range.startTime.getTime(),
      range.endTime.getTime(),
    ],
    queryFn: async () => {
      const result = await getExceptionLogs({
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        username,
        errorCode,
        startTimestamp: Math.floor(range.startTime.getTime() / 1000),
        endTimestamp: Math.floor(range.endTime.getTime() / 1000),
      })
      if (!result) {
        throw createServerError(result, t('Failed to load exceptions'))
      }
      return result
    },
    placeholderData: (previousData) => previousData,
  })

  const logs = useMemo(() => data?.items ?? [], [data])

  const appliedFilters = useMemo<ExceptionLogsFilters>(
    () => ({
      username,
      errorCode,
      startTime: range.startTime,
      endTime: range.endTime,
    }),
    [username, errorCode, range]
  )

  // 「搜索」把草稿写进地址栏，筛选条件变了才重新查询；条件没变就重拉当前页
  const applyFilters = useCallback(
    (next: ExceptionLogsFilters) => {
      const unchanged =
        next.username === appliedFilters.username &&
        next.errorCode === appliedFilters.errorCode &&
        next.startTime.getTime() === appliedFilters.startTime.getTime() &&
        next.endTime.getTime() === appliedFilters.endTime.getTime()
      if (unchanged) {
        void refetch()
        return
      }
      void navigate({
        to: '/exception-logs',
        search: (previous) => ({
          ...previous,
          filter: next.username || undefined,
          error_code: next.errorCode || undefined,
          startTime: next.startTime.getTime(),
          endTime: next.endTime.getTime(),
          page: 1,
        }),
      })
    },
    [appliedFilters, navigate, refetch]
  )

  // 自动刷新到点就点一次「搜索」：走同一个函数，条件没变时重拉当前页，
  // 也不把页码拽回第一页
  useAutoRefreshTick(autoRefresh.interval, () => {
    applyFilters(appliedFilters)
  })

  const { table } = useDataTable({
    data: logs,
    columns,
    columnFilters,
    globalFilter: username,
    pagination,
    onPaginationChange,
    // 筛选条件由地址栏驱动，表格本身不再改写它们
    onGlobalFilterChange: () => undefined,
    onColumnFiltersChange: () => undefined,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total ?? 0,
    ensurePageInRange,
    enableRowSelection: true,
  })

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No Failed Requests Found')}
        emptyDescription={t(
          'No failed request matches the current filters. Failures are recorded when error logging is enabled'
        )}
        skeletonKeyPrefix='exception-logs-skeleton'
        applyHeaderSize
        showMobileBulkActions
        mobileProps={{ enableRowSelection: true }}
        bulkActions={<ExceptionClearCapture table={table} />}
        toolbar={
          <ExceptionLogsFilterBar
            table={table}
            applied={appliedFilters}
            autoRefresh={autoRefresh}
            searchLoading={isFetching}
            onApply={applyFilters}
            onReset={() =>
              applyFilters({
                username: '',
                errorCode: '',
                startTime: defaultRange.start,
                endTime: defaultRange.end,
              })
            }
          />
        }
      />
      <ExceptionDetailDialog
        log={detailLog}
        open={detailOpen}
        onOpenChange={setDetailOpen}
      />
    </>
  )
}
