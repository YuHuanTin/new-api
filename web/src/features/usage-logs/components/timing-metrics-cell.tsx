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
import { Fragment } from 'react'
import { CircleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  dotColorMap,
  textColorMap,
  type StatusVariant,
} from '@/components/status-badge'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatUseTime } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  getFirstResponseTimeColor,
  getResponseTimeColor,
  getTimeColor,
} from '../lib/format'
import type { LogOtherData } from '../types'

/**
 * Softened fills for the full-height timing bar. The bar sits directly beside
 * dense numeric text, so the saturated `dotColorMap` tones (tuned for small
 * dots and badges) read as too high-contrast at that size; a translucent fill
 * keeps the status legible while matching the page's muted palette.
 */
const barColorMap: Record<StatusVariant, string> = {
  ...dotColorMap,
  success: 'bg-success/90',
  warning: 'bg-warning/80',
  danger: 'bg-destructive/80',
  neutral: 'bg-neutral/80',
}

interface TimingMetricsCellProps {
  useTimeSec: number
  completionTokens: number
  frtMs?: number
  /** 两次上游数据事件之间的最长静默时间（毫秒），缺失或为 0 时不显示 */
  maxIdleMs?: number | null
  isStream: boolean
  className?: string
  /**
   * `bar` (default) draws a full-height color segment beside the labels,
   * matching the dense desktop table. `dot` swaps that segment for small
   * status dots inline with each label, matching the lighter-weight status
   * indicator used elsewhere on the mobile card.
   */
  indicator?: 'bar' | 'dot'
  compact?: boolean
}

export function TimingMetricsCell(props: TimingMetricsCellProps) {
  const { t } = useTranslation()
  const indicator = props.indicator ?? 'bar'
  const showFirstToken = props.isStream
  const firstTokenSeconds =
    props.frtMs != null && props.frtMs > 0 ? props.frtMs / 1000 : null
  const firstTokenVariant: StatusVariant =
    firstTokenSeconds == null
      ? 'neutral'
      : getFirstResponseTimeColor(firstTokenSeconds)
  const totalTimeVariant = getResponseTimeColor(
    props.useTimeSec,
    props.completionTokens
  )
  const firstTokenLabel =
    firstTokenSeconds == null ? t('N/A') : formatUseTime(firstTokenSeconds)
  const totalTimeLabel = formatUseTime(props.useTimeSec)
  const maxIdleSeconds =
    props.maxIdleMs != null && props.maxIdleMs > 0
      ? props.maxIdleMs / 1000
      : null
  const maxIdleLabel =
    maxIdleSeconds == null ? null : formatUseTime(maxIdleSeconds)
  const maxIdleVariant: StatusVariant =
    maxIdleSeconds == null ? 'neutral' : getTimeColor(maxIdleSeconds)

  // 三个指标共用一组行，数值列纵向对齐
  const rows: {
    key: string
    label: string
    value: string
    variant: StatusVariant
    title?: string
  }[] = []
  if (showFirstToken) {
    rows.push({
      key: 'first-token',
      label: t('First token'),
      value: firstTokenLabel,
      variant: firstTokenVariant,
    })
  }
  rows.push({
    key: 'duration',
    label: t('Duration'),
    value: totalTimeLabel,
    variant: totalTimeVariant,
  })
  if (maxIdleLabel) {
    rows.push({
      key: 'max-idle',
      label: t('MIT'),
      value: maxIdleLabel,
      variant: maxIdleVariant,
      title: t('Maximum idle time'),
    })
  }

  const labels = props.compact ? (
    <div className='flex min-h-0 min-w-0 flex-row flex-wrap items-center gap-x-2.5 gap-y-1 text-xs leading-tight'>
      {rows.map((row) => (
        <div key={row.key} className='flex items-baseline gap-1.5'>
          {indicator === 'dot' && (
            <span
              aria-hidden
              className={cn(
                'size-1.5 shrink-0 rounded-full',
                dotColorMap[row.variant]
              )}
            />
          )}
          <span className='text-muted-foreground shrink-0'>{row.label}</span>
          <span
            className={cn('tabular-nums', textColorMap[row.variant])}
            title={row.title}
          >
            {row.value}
          </span>
        </div>
      ))}
    </div>
  ) : (
    // 网格固定标签列与数值列，行距压紧
    <div
      className={cn(
        'grid min-w-0 content-center gap-x-1.5 text-[11px] leading-[1.05]',
        indicator === 'dot'
          ? 'grid-cols-[auto_auto_auto]'
          : 'grid-cols-[auto_auto]'
      )}
    >
      {rows.map((row) => (
        <Fragment key={row.key}>
          {indicator === 'dot' && (
            <span
              aria-hidden
              className={cn(
                'size-1.5 shrink-0 self-center rounded-full',
                dotColorMap[row.variant]
              )}
            />
          )}
          <span className='text-muted-foreground shrink-0'>{row.label}</span>
          <span
            className={cn('tabular-nums', textColorMap[row.variant])}
            title={row.title}
          >
            {row.value}
          </span>
        </Fragment>
      ))}
    </div>
  )

  if (indicator === 'dot') {
    return (
      <div className={cn('flex items-stretch', props.className)}>{labels}</div>
    )
  }

  return (
    <div className={cn('flex items-stretch gap-2', props.className)}>
      <span
        aria-hidden
        className={cn(
          'flex w-1 shrink-0 flex-col overflow-hidden rounded-full',
          !showFirstToken && barColorMap[totalTimeVariant]
        )}
      >
        {showFirstToken && (
          <>
            <span className={cn('flex-1', barColorMap[firstTokenVariant])} />
            <span className={cn('flex-1', barColorMap[totalTimeVariant])} />
            {maxIdleLabel && (
              <span className={cn('flex-1', barColorMap[maxIdleVariant])} />
            )}
          </>
        )}
      </span>
      {labels}
    </div>
  )
}

interface StreamTpsCellProps {
  isStream: boolean
  compact?: boolean
  /** Task logs are asynchronous jobs; stream vs non-stream does not apply. */
  isTask?: boolean
  /** The task request returned its result inline, so it is a synchronous call. */
  isSyncTask?: boolean
  tokensPerSecond?: number | null
  streamStatus?: LogOtherData['stream_status']
  className?: string
}

export function StreamTpsCell(props: StreamTpsCellProps) {
  const { t } = useTranslation()
  const showStreamError =
    props.isStream && props.streamStatus && props.streamStatus.status !== 'ok'
  const tpsLabel =
    props.tokensPerSecond != null
      ? `${Math.round(props.tokensPerSecond)} t/s`
      : '—'
  let streamLabel = props.isStream ? t('Stream') : t('Non-stream')
  if (props.isTask) {
    streamLabel = props.isSyncTask ? t('Sync') : t('Async')
  }

  return (
    <div
      className={cn(
        'flex shrink-0 flex-col items-start justify-center gap-0.5 text-xs leading-tight',
        props.compact && 'flex-row flex-wrap items-center gap-1.5',
        props.className
      )}
    >
      <span
        className={cn(
          'inline-flex items-center gap-1 font-medium',
          props.isStream ? 'text-info' : 'text-muted-foreground'
        )}
      >
        {streamLabel}
        {showStreamError && (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger
                render={<CircleAlert className='text-destructive size-3' />}
              />
              <TooltipContent>
                <div className='space-y-0.5 text-xs'>
                  <p>
                    {t('Stream Status')}: {t('Error')}
                  </p>
                  <p>{props.streamStatus?.end_reason || 'unknown'}</p>
                  {props.streamStatus?.error_code && (
                    <p>
                      {t('Upstream Error Code')}: {props.streamStatus.error_code}
                    </p>
                  )}
                  {(props.streamStatus?.error_count ?? 0) > 0 && (
                    <p>
                      {t('Soft Errors')}: {props.streamStatus?.error_count}
                    </p>
                  )}
                </div>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
      </span>
      {(!props.compact ||
        (props.isStream && props.tokensPerSecond != null)) && (
        <span className='text-muted-foreground/60 px-0.5 tabular-nums'>
          {tpsLabel}
        </span>
      )}
    </div>
  )
}
