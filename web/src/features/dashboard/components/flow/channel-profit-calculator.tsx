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
import { Calculator, RotateCcw } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { IconBadge } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { FlowQuotaDataItem } from '@/features/dashboard/types'
import { toIntlLocale } from '@/i18n/languages'
import { formatLocalCurrencyAmount } from '@/lib/currency'
import { formatNumber, formatPercentOneDecimal } from '@/lib/format'
import { cn } from '@/lib/utils'
import {
  useSystemConfigStore,
  type CurrencyConfig,
} from '@/stores/system-config-store'

type CostMode = 'amount' | 'ratio'

interface ChannelCostPref {
  mode: CostMode
  ratio: string
}

interface ChannelProfitRow {
  channelId: number
  channelName: string
  requests: number
  quota: number
  revenue: number
}

interface ChannelProfitCalculatorProps {
  rows: FlowQuotaDataItem[]
  selectedUsers: string[]
  rangeKey: string
  maskSensitive?: boolean
}

const CHANNEL_PREFS_STORAGE_KEY = 'new-api:flow-profit:channel-prefs'
const RANGE_AMOUNTS_STORAGE_PREFIX = 'new-api:flow-profit:range-amounts:'

function readStorageJSON<T>(key: string, fallback: T): T {
  try {
    const raw = window.localStorage.getItem(key)
    if (!raw) return fallback
    return JSON.parse(raw) as T
  } catch {
    return fallback
  }
}

function writeStorageJSON(key: string, value: unknown): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // 忽略隐私模式或配额不足导致的存储写入失败
  }
}

function resolveCurrencyMeta(currency: CurrencyConfig): {
  quotaPerUnit: number
  exchangeRate: number
  symbol: string
} {
  const quotaPerUnit = currency.quotaPerUnit > 0 ? currency.quotaPerUnit : 500000
  if (currency.quotaDisplayType === 'CNY') {
    return {
      quotaPerUnit,
      exchangeRate:
        currency.usdExchangeRate > 0 ? currency.usdExchangeRate : 1,
      symbol: '¥',
    }
  }
  if (currency.quotaDisplayType === 'CUSTOM') {
    return {
      quotaPerUnit,
      exchangeRate:
        currency.customCurrencyExchangeRate > 0
          ? currency.customCurrencyExchangeRate
          : 1,
      symbol: currency.customCurrencySymbol || '¤',
    }
  }
  return {
    quotaPerUnit,
    exchangeRate: 1,
    symbol: '$',
  }
}

function maskChannelLabel(name: string): string {
  if (name.length <= 2) return '***'
  return `${name.slice(0, 2)}***`
}

function parseNonNegativeInput(raw: string): number | undefined {
  const trimmed = raw.trim()
  if (trimmed === '') return undefined
  const num = Number(trimmed)
  if (!Number.isFinite(num) || num < 0) return undefined
  return num
}

export function ChannelProfitCalculator(props: ChannelProfitCalculatorProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const currency = useSystemConfigStore((state) => state.config.currency)
  const currencyMeta = useMemo(() => resolveCurrencyMeta(currency), [currency])

  // 按渠道持久化「成本模式 + 默认进货倍率」，换日期区间自动保留
  const [channelPrefs, setChannelPrefs] = useState<
    Record<string, ChannelCostPref>
  >(() => readStorageJSON(CHANNEL_PREFS_STORAGE_KEY, {}))

  // 按时间区间持久化「手填上游消耗金额」，切换今天/昨天各自独立
  const rangeStorageKey = `${RANGE_AMOUNTS_STORAGE_PREFIX}${props.rangeKey}`
  const [rangeAmounts, setRangeAmounts] = useState<Record<string, string>>(() =>
    readStorageJSON(rangeStorageKey, {})
  )

  useEffect(() => {
    setRangeAmounts(readStorageJSON(rangeStorageKey, {}))
  }, [rangeStorageKey])

  // 复用分流数据按渠道聚合平台收入（同步遵循已选用户筛选）
  const channels = useMemo<ChannelProfitRow[]>(() => {
    const userSet =
      props.selectedUsers.length > 0 ? new Set(props.selectedUsers) : undefined
    const grouped = new Map<number, ChannelProfitRow>()

    for (const item of props.rows) {
      if (userSet) {
        const userKey = item.username || String(item.user_id ?? '')
        if (!userSet.has(userKey)) continue
      }
      const channelId = item.channel_id ?? 0
      if (channelId <= 0) continue

      const existing = grouped.get(channelId)
      const itemQuota = item.quota ?? 0
      const itemRequests = item.count ?? 0
      if (existing) {
        existing.quota += itemQuota
        existing.requests += itemRequests
      } else {
        const rawName = item.channel_name?.trim() || `#${channelId}`
        grouped.set(channelId, {
          channelId,
          channelName: props.maskSensitive
            ? maskChannelLabel(rawName)
            : rawName,
          requests: itemRequests,
          quota: itemQuota,
          revenue: 0,
        })
      }
    }

    const list = [...grouped.values()].map((row) => ({
      ...row,
      revenue: (row.quota / currencyMeta.quotaPerUnit) * currencyMeta.exchangeRate,
    }))
    list.sort((a, b) => b.quota - a.quota)
    return list
  }, [
    currencyMeta.exchangeRate,
    currencyMeta.quotaPerUnit,
    props.maskSensitive,
    props.rows,
    props.selectedUsers,
  ])

  const updateChannelPref = useCallback(
    (channelId: number, patch: Partial<ChannelCostPref>) => {
      const key = String(channelId)
      setChannelPrefs((prev) => {
        const current = prev[key] ?? { mode: 'amount', ratio: '' }
        const next = {
          ...prev,
          [key]: { ...current, ...patch },
        }
        writeStorageJSON(CHANNEL_PREFS_STORAGE_KEY, next)
        return next
      })
    },
    []
  )

  const updateRangeAmount = useCallback(
    (channelId: number, value: string) => {
      const key = String(channelId)
      setRangeAmounts((prev) => {
        const next = { ...prev, [key]: value }
        writeStorageJSON(rangeStorageKey, next)
        return next
      })
    },
    [rangeStorageKey]
  )

  const handleResetAmounts = useCallback(() => {
    setRangeAmounts({})
    writeStorageJSON(rangeStorageKey, {})
  }, [rangeStorageKey])

  const computedRows = useMemo(() => {
    return channels.map((row) => {
      const key = String(row.channelId)
      const pref = channelPrefs[key] ?? { mode: 'amount' as CostMode, ratio: '' }
      const rawAmount = rangeAmounts[key] ?? ''

      let cost: number | undefined
      if (pref.mode === 'ratio') {
        const ratio = parseNonNegativeInput(pref.ratio)
        if (ratio !== undefined) {
          cost = row.revenue * ratio
        }
      } else {
        cost = parseNonNegativeInput(rawAmount)
      }

      const profit = cost !== undefined ? row.revenue - cost : undefined
      const margin =
        profit !== undefined && row.revenue > 0
          ? (profit / row.revenue) * 100
          : undefined

      return {
        ...row,
        mode: pref.mode,
        ratioInput: pref.ratio,
        amountInput: rawAmount,
        cost,
        profit,
        margin,
      }
    })
  }, [channelPrefs, channels, rangeAmounts])

  const summary = useMemo(() => {
    let totalRevenue = 0
    let totalCost = 0
    let filledCount = 0
    for (const row of computedRows) {
      totalRevenue += row.revenue
      if (row.cost !== undefined) {
        totalCost += row.cost
        filledCount += 1
      }
    }
    const hasAnyCost = filledCount > 0
    const totalProfit = hasAnyCost ? totalRevenue - totalCost : undefined
    const totalMargin =
      totalProfit !== undefined && totalRevenue > 0
        ? (totalProfit / totalRevenue) * 100
        : undefined
    return {
      totalRevenue,
      totalCost,
      totalProfit,
      totalMargin,
      filledCount,
      totalCount: computedRows.length,
    }
  }, [computedRows])

  const formatMoney = useCallback(
    (value: number) => formatLocalCurrencyAmount(value, { locale }),
    [locale]
  )

  const formatSignedProfit = useCallback(
    (value: number) => {
      const formatted = formatLocalCurrencyAmount(Math.abs(value), { locale })
      if (value > 0) return `+${formatted}`
      if (value < 0) return `-${formatted}`
      return formatted
    },
    [locale]
  )

  const formatMargin = useCallback(
    (value: number) => formatPercentOneDecimal(value, locale),
    [locale]
  )

  const copySummaryText = useMemo(() => {
    const lines = computedRows.map((row) => {
      const rev = formatMoney(row.revenue)
      const cost = row.cost !== undefined ? formatMoney(row.cost) : '-'
      const profit =
        row.profit !== undefined ? formatSignedProfit(row.profit) : '-'
      const margin = row.margin !== undefined ? formatMargin(row.margin) : '-'
      return `${row.channelName} (#${row.channelId}) | ${t('Revenue')}: ${rev} | ${t('Upstream cost')}: ${cost} | ${t('Profit')}: ${profit} (${margin})`
    })
    const totalProfitText =
      summary.totalProfit !== undefined
        ? formatSignedProfit(summary.totalProfit)
        : '-'
    const totalMarginText =
      summary.totalMargin !== undefined
        ? formatMargin(summary.totalMargin)
        : '-'
    lines.push(
      `---`,
      `${t('Total')} (${summary.filledCount}/${summary.totalCount}) | ${t('Revenue')}: ${formatMoney(summary.totalRevenue)} | ${t('Upstream cost')}: ${formatMoney(summary.totalCost)} | ${t('Profit')}: ${totalProfitText} (${totalMarginText})`
    )
    return lines.join('\n')
  }, [
    computedRows,
    formatMargin,
    formatMoney,
    formatSignedProfit,
    summary,
    t,
  ])

  if (channels.length === 0) {
    return null
  }

  let profitBadgeClass = 'text-muted-foreground'
  if (summary.totalProfit !== undefined && summary.totalProfit > 0) {
    profitBadgeClass = 'text-emerald-600 dark:text-emerald-400'
  } else if (summary.totalProfit !== undefined && summary.totalProfit < 0) {
    profitBadgeClass = 'text-destructive'
  }

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex flex-col gap-3 border-b px-3 py-2.5 sm:px-5 sm:py-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='flex min-w-0 items-center gap-2'>
          <IconBadge tone='info' size='sm'>
            <Calculator />
          </IconBadge>
          <div className='min-w-0'>
            <div className='text-sm font-semibold'>
              {t('Channel Profit Calculator')}
            </div>
            <div className='text-muted-foreground text-xs'>
              {t(
                'Enter daily upstream cost or a fixed cost ratio to calculate profit automatically.'
              )}
            </div>
          </div>
        </div>

        <div className='flex flex-wrap items-center gap-2'>
          <div className='bg-muted/40 flex flex-wrap items-center gap-3 rounded-md border px-3 py-1.5 text-xs'>
            <span className='text-muted-foreground'>
              {t('Revenue')}:{' '}
              <strong className='text-foreground font-mono'>
                {formatMoney(summary.totalRevenue)}
              </strong>
            </span>
            <span className='text-muted-foreground'>
              {t('Upstream cost')}:{' '}
              <strong className='text-foreground font-mono'>
                {summary.filledCount > 0 ? formatMoney(summary.totalCost) : '-'}
              </strong>
            </span>
            <span className='text-muted-foreground'>
              {t('Profit')}:{' '}
              <strong className={cn('font-mono', profitBadgeClass)}>
                {summary.totalProfit !== undefined
                  ? formatSignedProfit(summary.totalProfit)
                  : '-'}
              </strong>
              {summary.totalMargin !== undefined && (
                <span className={cn('ml-1 font-mono', profitBadgeClass)}>
                  ({formatMargin(summary.totalMargin)})
                </span>
              )}
            </span>
          </div>

          <CopyButton
            value={copySummaryText}
            variant='outline'
            size='sm'
            tooltip={t('Copy summary')}
            className='gap-1.5 text-xs'
          >
            {t('Copy summary')}
          </CopyButton>

          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleResetAmounts}
            className='gap-1.5 text-xs'
          >
            <RotateCcw className='size-3.5' />
            {t('Clear costs')}
          </Button>
        </div>
      </div>

      <div className='overflow-x-auto'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='min-w-[180px]'>{t('Channel')}</TableHead>
              <TableHead className='text-right'>{t('Requests')}</TableHead>
              <TableHead className='text-right'>{t('Revenue')}</TableHead>
              <TableHead className='w-[150px]'>{t('Cost mode')}</TableHead>
              <TableHead className='w-[220px]'>{t('Upstream cost')}</TableHead>
              <TableHead className='text-right'>{t('Profit')}</TableHead>
              <TableHead className='text-right'>{t('Profit margin')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {computedRows.map((row) => {
              let rowProfitClass = 'text-muted-foreground'
              if (row.profit !== undefined && row.profit > 0) {
                rowProfitClass = 'text-emerald-600 dark:text-emerald-400'
              } else if (row.profit !== undefined && row.profit < 0) {
                rowProfitClass = 'text-destructive'
              }

              return (
                <TableRow key={row.channelId}>
                  <TableCell>
                    <div className='flex items-center gap-2'>
                      <span className='font-medium'>{row.channelName}</span>
                      <Badge variant='secondary' className='font-mono text-[11px]'>
                        #{row.channelId}
                      </Badge>
                    </div>
                  </TableCell>
                  <TableCell className='text-muted-foreground text-right font-mono text-xs'>
                    {formatNumber(row.requests, locale)}
                  </TableCell>
                  <TableCell className='text-right font-mono'>
                    {formatMoney(row.revenue)}
                  </TableCell>
                  <TableCell>
                    <Tabs
                      value={row.mode}
                      onValueChange={(value) =>
                        updateChannelPref(row.channelId, {
                          mode: value as CostMode,
                        })
                      }
                    >
                      <TabsList className='h-7'>
                        <TabsTrigger value='amount' className='px-2 text-xs'>
                          {t('Amount')}
                        </TabsTrigger>
                        <TabsTrigger value='ratio' className='px-2 text-xs'>
                          {t('Ratio')}
                        </TabsTrigger>
                      </TabsList>
                    </Tabs>
                  </TableCell>
                  <TableCell>
                    {row.mode === 'amount' ? (
                      <div className='relative flex items-center'>
                        <span className='text-muted-foreground pointer-events-none absolute left-2.5 font-mono text-xs'>
                          {currencyMeta.symbol}
                        </span>
                        <Input
                          type='number'
                          inputMode='decimal'
                          min='0'
                          step='any'
                          value={row.amountInput}
                          onChange={(event) =>
                            updateRangeAmount(row.channelId, event.target.value)
                          }
                          placeholder='0.00'
                          aria-label={`${row.channelName} ${t('Upstream cost')}`}
                          className='h-8 pl-6 font-mono text-xs'
                        />
                      </div>
                    ) : (
                      <div className='flex items-center gap-2'>
                        <div className='relative flex w-24 shrink-0 items-center'>
                          <Input
                            type='number'
                            inputMode='decimal'
                            min='0'
                            step='0.01'
                            value={row.ratioInput}
                            onChange={(event) =>
                              updateChannelPref(row.channelId, {
                                ratio: event.target.value,
                              })
                            }
                            placeholder='0.45'
                            aria-label={`${row.channelName} ${t('Cost ratio')}`}
                            className='h-8 pr-6 font-mono text-xs'
                          />
                          <span className='text-muted-foreground pointer-events-none absolute right-2 font-mono text-xs'>
                            ×
                          </span>
                        </div>
                        <span className='text-muted-foreground truncate font-mono text-xs'>
                          {row.cost !== undefined
                            ? `= ${formatMoney(row.cost)}`
                            : '-'}
                        </span>
                      </div>
                    )}
                  </TableCell>
                  <TableCell
                    className={cn('text-right font-mono font-medium', rowProfitClass)}
                  >
                    {row.profit !== undefined
                      ? formatSignedProfit(row.profit)
                      : '-'}
                  </TableCell>
                  <TableCell
                    className={cn('text-right font-mono text-xs', rowProfitClass)}
                  >
                    {row.margin !== undefined ? formatMargin(row.margin) : '-'}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
          <TableFooter>
            <TableRow>
              <TableCell className='font-semibold'>
                {t('Total')} ({summary.filledCount}/{summary.totalCount})
              </TableCell>
              <TableCell className='text-right font-mono text-xs'>
                {formatNumber(
                  computedRows.reduce((acc, row) => acc + row.requests, 0),
                  locale
                )}
              </TableCell>
              <TableCell className='text-right font-mono font-semibold'>
                {formatMoney(summary.totalRevenue)}
              </TableCell>
              <TableCell />
              <TableCell className='font-mono font-semibold'>
                {summary.filledCount > 0 ? formatMoney(summary.totalCost) : '-'}
              </TableCell>
              <TableCell
                className={cn(
                  'text-right font-mono font-semibold',
                  profitBadgeClass
                )}
              >
                {summary.totalProfit !== undefined
                  ? formatSignedProfit(summary.totalProfit)
                  : '-'}
              </TableCell>
              <TableCell
                className={cn(
                  'text-right font-mono text-xs font-semibold',
                  profitBadgeClass
                )}
              >
                {summary.totalMargin !== undefined
                  ? formatMargin(summary.totalMargin)
                  : '-'}
              </TableCell>
            </TableRow>
          </TableFooter>
        </Table>
      </div>
    </div>
  )
}
