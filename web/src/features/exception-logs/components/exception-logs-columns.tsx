import type { ColumnDef } from '@tanstack/react-table'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { formatTimestampToDate } from '@/lib/format'

import { parseExceptionLogOther, type ExceptionLog } from '../types'

const LOG_TYPE_ERROR = 5

/** 从日志的 other 里取出这次失败最要紧的两个信息：错误码和错误类型。 */
export function getExceptionErrorInfo(log: ExceptionLog): {
  code?: string
  type?: string
  status?: number
} {
  const other = parseExceptionLogOther(log.other)
  const streamStatus = other.stream_status
  return {
    code: other.error_code ?? streamStatus?.error_code,
    type: other.error_type ?? streamStatus?.error_type,
    status: other.status_code,
  }
}

export function useExceptionLogsColumns(
  onOpenDetails: (log: ExceptionLog) => void
): ColumnDef<ExceptionLog>[] {
  const { t } = useTranslation()
  return useMemo<ColumnDef<ExceptionLog>[]>(
    () => [
      {
        id: 'select',
        header: ({ table }) => (
          <Checkbox
            checked={table.getIsAllPageRowsSelected()}
            indeterminate={table.getIsSomePageRowsSelected()}
            onCheckedChange={(value) =>
              table.toggleAllPageRowsSelected(!!value)
            }
            aria-label={t('Select all')}
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            checked={row.getIsSelected()}
            onCheckedChange={(value) => row.toggleSelected(!!value)}
            aria-label={t('Select row')}
          />
        ),
        enableSorting: false,
        enableHiding: false,
        enableResizing: false,
        // 手机端由卡片列表自己的勾选框负责，列本身不再重复渲染
        meta: { mobileHidden: true },
        size: 40,
      },
      {
        accessorKey: 'created_at',
        header: t('Time'),
        cell: ({ row }) => (
          <div className='flex min-w-0 flex-col gap-0.5'>
            <span className='truncate font-mono text-xs tabular-nums'>
              {formatTimestampToDate(row.getValue('created_at') as number)}
            </span>
            <StatusBadge
              label={t(
                row.original.type === LOG_TYPE_ERROR
                  ? 'Error'
                  : 'Failed Request'
              )}
              variant={row.original.type === LOG_TYPE_ERROR ? 'red' : 'orange'}
              size='sm'
              copyable={false}
              className='-ml-1.5 !text-xs [&_span]:!text-xs'
            />
          </div>
        ),
        enableHiding: false,
        size: 180,
      },
      {
        accessorKey: 'username',
        header: t('User'),
        cell: ({ row }) => (
          <span className='truncate text-sm'>
            {row.original.username || '-'}
          </span>
        ),
        size: 140,
      },
      {
        accessorKey: 'channel_name',
        header: t('Channel'),
        cell: ({ row }) => (
          <span className='truncate text-sm'>
            {row.original.channel_name || `#${row.original.channel_id || '-'}`}
          </span>
        ),
        size: 140,
      },
      {
        accessorKey: 'model_name',
        header: t('Model'),
        cell: ({ row }) => (
          <span className='truncate text-sm'>
            {row.original.model_name || '-'}
          </span>
        ),
        size: 160,
      },
      {
        id: 'error_code',
        header: t('Error Code'),
        cell: ({ row }) => {
          const info = getExceptionErrorInfo(row.original)
          if (!info.code) {
            return <span className='text-muted-foreground text-sm'>-</span>
          }
          return (
            <StatusBadge
              label={info.code}
              variant='red'
              size='sm'
              copyable
              copyText={info.code}
            />
          )
        },
        size: 180,
      },
      {
        id: 'error_type',
        header: t('Error Type'),
        cell: ({ row }) => {
          const info = getExceptionErrorInfo(row.original)
          return (
            <span className='text-muted-foreground truncate text-sm'>
              {info.type || '-'}
              {info.status ? ` (${info.status})` : ''}
            </span>
          )
        },
        size: 160,
      },
      {
        id: 'actions',
        header: '',
        cell: ({ row }) => (
          <Button
            variant='outline'
            size='sm'
            onClick={() => onOpenDetails(row.original)}
          >
            {t('Details')}
          </Button>
        ),
        enableHiding: false,
        size: 100,
      },
    ],
    [onOpenDetails, t]
  )
}
