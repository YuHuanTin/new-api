import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Table } from '@tanstack/react-table'
import { Eraser } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'

import { clearExceptionCapture } from '../api'
import type { ExceptionLog } from '../types'

type ExceptionClearCaptureProps = {
  table: Table<ExceptionLog>
}

/**
 * 选中若干失败请求后，清掉它们保存的抓包现场来释放存储空间。日志记录本身
 * 保留，列表只会在清理后重新拉取一次
 */
export function ExceptionClearCapture({ table }: ExceptionClearCaptureProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const selectedRows = table.getFilteredSelectedRowModel().rows

  const mutation = useMutation({
    mutationFn: (ids: number[]) => clearExceptionCapture(ids),
    onSuccess: async (result) => {
      setConfirmOpen(false)
      table.resetRowSelection()
      toast.success(
        t('Cleared capture data for {{count}} requests', {
          count: result.cleared,
        })
      )
      await queryClient.invalidateQueries({ queryKey: ['exception-logs'] })
    },
    onError: (error) => {
      handleServerError(error, t('Failed to clear capture data'))
    },
  })

  const handleConfirm = () => {
    const ids = selectedRows.map((row) => row.original.id)
    if (ids.length === 0) return
    mutation.mutate(ids)
  }

  return (
    <>
      <DataTableBulkActions table={table} entityName={t('Request')}>
        <Button
          variant='destructive'
          size='sm'
          className='gap-1.5'
          disabled={mutation.isPending}
          onClick={() => setConfirmOpen(true)}
        >
          <Eraser className='size-4' />
          {t('Clear capture')}
        </Button>
      </DataTableBulkActions>
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Clear captured data?')}
        desc={t(
          'Removes the saved request and response payloads of the {{count}} selected requests. The log records themselves are kept',
          { count: selectedRows.length }
        )}
        destructive
        isLoading={mutation.isPending}
        confirmText={t('Clear')}
        handleConfirm={handleConfirm}
      />
    </>
  )
}
