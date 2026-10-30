import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
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

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: 20 },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      { columnId: 'error_code', searchKey: 'error_code', type: 'string' },
    ],
  })

  const errorCode =
    (columnFilters.find((filter) => filter.id === 'error_code')?.value as
      | string
      | undefined) ?? ''

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
      globalFilter,
      errorCode,
    ],
    queryFn: async () => {
      const result = await getExceptionLogs({
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        username: globalFilter,
        errorCode,
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
    () => ({ username: globalFilter ?? '', errorCode }),
    [globalFilter, errorCode]
  )

  // 「搜索」把草稿写进地址栏，筛选条件变了才重新查询；条件没变就重拉当前页
  const applyFilters = useCallback(
    (next: ExceptionLogsFilters) => {
      const unchanged =
        next.username === appliedFilters.username &&
        next.errorCode === appliedFilters.errorCode
      if (unchanged) {
        void refetch()
        return
      }
      onGlobalFilterChange?.(next.username)
      onColumnFiltersChange(
        next.errorCode ? [{ id: 'error_code', value: next.errorCode }] : []
      )
    },
    [appliedFilters, onColumnFiltersChange, onGlobalFilterChange, refetch]
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
    globalFilter,
    pagination,
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
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
            onReset={() => {
              onGlobalFilterChange?.('')
              onColumnFiltersChange([])
            }}
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
