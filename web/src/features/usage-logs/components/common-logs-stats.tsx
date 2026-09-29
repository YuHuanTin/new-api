/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { formatLogQuota, formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getAllLogs, getLogStats, getUserLogStats, getUserLogs } from '../api'
import { DEFAULT_LOG_STATS } from '../constants'
import type { UsageLog } from '../data/schema'
import {
  calculateCommonLogTokenStats,
  mergeCommonLogTokenStats,
} from '../lib/stats'
import { buildApiParams } from '../lib/utils'
import { useLogsViewScope, useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')
const STATS_LOG_PAGE_SIZE = 100
const STATS_LOG_REQUEST_CONCURRENCY = 5

async function fetchFilteredLogsForStats(
  isAdmin: boolean,
  searchParams: Record<string, unknown>
): Promise<ReturnType<typeof calculateCommonLogTokenStats>> {
  const fetchPage = async (page: number) => {
    const params = buildApiParams({
      page,
      pageSize: STATS_LOG_PAGE_SIZE,
      searchParams,
      columnFilters: [],
      isAdmin,
    })
    const response = isAdmin
      ? await getAllLogs(params)
      : await getUserLogs(params)
    const result = requireServerSuccess(response)

    return {
      stats: calculateCommonLogTokenStats(
        (result.data?.items ?? []) as UsageLog[]
      ),
      total: result.data?.total ?? 0,
    }
  }

  const firstPage = await fetchPage(1)
  const pageCount = Math.ceil(firstPage.total / STATS_LOG_PAGE_SIZE)
  if (pageCount <= 1) return firstPage.stats

  let stats = firstPage.stats
  for (
    let firstBatchPage = 2;
    firstBatchPage <= pageCount;
    firstBatchPage += STATS_LOG_REQUEST_CONCURRENCY
  ) {
    const pageBatch = await Promise.all(
      Array.from(
        {
          length: Math.min(
            STATS_LOG_REQUEST_CONCURRENCY,
            pageCount - firstBatchPage + 1
          ),
        },
        (_, index) => fetchPage(firstBatchPage + index)
      )
    )
    stats = mergeCommonLogTokenStats(
      stats,
      ...pageBatch.map((page) => page.stats)
    )
  }

  return stats
}

function StatBadge(props: {
  label: string
  value: string | number
  accent: string
}) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 rounded-full', props.accent)} />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
    </span>
  )
}

export function CommonLogsStats() {
  const { t, i18n } = useTranslation()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const searchParams = route.useSearch()
  const { sensitiveVisible } = useUsageLogsContext()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  const { data: stats, isLoading } = useQuery({
    queryKey: ['usage-logs-stats', isAdmin, searchParams],
    queryFn: async () => {
      const params = buildApiParams({
        page: 1,
        pageSize: 1,
        searchParams,
        columnFilters: [],
        isAdmin,
      })

      const result = isAdmin
        ? requireServerSuccess(await getLogStats(params))
        : requireServerSuccess(await getUserLogStats(params))

      return result.success
        ? result.data || DEFAULT_LOG_STATS
        : DEFAULT_LOG_STATS
    },
    placeholderData: (previousData) => previousData,
  })

  const { data: tokenStats } = useQuery({
    queryKey: [
      'usage-logs-token-stats',
      isAdmin,
      {
        startTime: searchParams.startTime,
        endTime: searchParams.endTime,
        type: searchParams.type,
        model: searchParams.model,
        token: searchParams.token,
        group: searchParams.group,
        channel: searchParams.channel,
        username: searchParams.username,
        requestId: searchParams.requestId,
        upstreamRequestId: searchParams.upstreamRequestId,
      },
    ],
    queryFn: () => fetchFilteredLogsForStats(isAdmin, searchParams),
    staleTime: 30_000,
  })

  if (isLoading) {
    return (
      <div className='flex items-center gap-2'>
        <Skeleton className='h-7 w-[150px] rounded-md' />
        <Skeleton className='h-7 w-[100px] rounded-md' />
        <Skeleton className='h-7 w-[120px] rounded-md' />
        <Skeleton className='h-7 w-[120px] rounded-md' />
        <Skeleton className='h-7 w-[140px] rounded-md' />
      </div>
    )
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <StatBadge
        label={t('Usage')}
        value={sensitiveVisible ? formatLogQuota(stats?.quota || 0) : '••••'}
        accent='bg-sky-500/70'
      />
      <StatBadge
        label={t('RPM')}
        value={stats?.rpm || 0}
        accent='bg-rose-500/65'
      />
      <StatBadge
        label={t('TPM')}
        value={stats?.tpm || 0}
        accent='bg-slate-400/70'
      />
      <StatBadge
        label={t('Cache Rate')}
        value={
          tokenStats ? `${formatNumber(tokenStats.cacheRate, locale)}%` : '-'
        }
        accent='bg-emerald-500/65'
      />
      <StatBadge
        label={t('Total Tokens')}
        value={formatNumber(tokenStats?.totalTokens, locale)}
        accent='bg-amber-500/65'
      />
    </div>
  )
}
