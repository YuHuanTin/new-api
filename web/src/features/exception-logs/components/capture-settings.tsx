import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, Eraser } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import {
  clearAllExceptionCapture,
  getCaptureSetting,
  getCaptureUsage,
  readCaptureSetting,
  saveCaptureSetting,
  writeCaptureSetting,
} from '../api'
import type { CaptureSetting } from '../types'

const DEFAULT_SETTING: CaptureSetting = { retention_days: 3 }

// 字节数按 1024 进位，保留一位小数
function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`
  }
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(1)} ${units[unit]}`
}

// 抓包数据保留时长，超过后由后台任务清理
export function CaptureSettings() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [clearAllOpen, setClearAllOpen] = useState(false)
  const [setting, setSetting] = useState<CaptureSetting>(DEFAULT_SETTING)

  const { data } = useQuery({
    queryKey: ['capture-setting'],
    queryFn: async () => {
      const result = await getCaptureSetting()
      return readCaptureSetting(result.options)
    },
  })

  // 展开面板时才统计，避免每次进页面都扫一遍日志表
  const usageQuery = useQuery({
    queryKey: ['capture-usage'],
    queryFn: getCaptureUsage,
    enabled: open,
  })

  useEffect(() => {
    if (data) {
      setSetting(data)
    }
  }, [data])

  const saveMutation = useMutation({
    mutationFn: async (next: CaptureSetting) =>
      saveCaptureSetting(writeCaptureSetting(next)),
    onSuccess: async () => {
      toast.success(t('Capture setting saved'))
      await queryClient.invalidateQueries({ queryKey: ['capture-setting'] })
    },
    onError: (error) => {
      handleServerError(error, t('Failed to save capture setting'))
    },
  })

  // 清理所有抓包数据，日志记录本身保留
  const clearAllMutation = useMutation({
    mutationFn: clearAllExceptionCapture,
    onSuccess: async (result) => {
      setClearAllOpen(false)
      toast.success(
        t('Cleared capture data for {{count}} requests', {
          count: result.cleared,
        })
      )
      await queryClient.invalidateQueries({ queryKey: ['exception-logs'] })
      await queryClient.invalidateQueries({ queryKey: ['exception-capture'] })
      await queryClient.invalidateQueries({ queryKey: ['capture-usage'] })
    },
    onError: (error) => {
      handleServerError(error, t('Failed to clear capture data'))
    },
  })

  return (
    <>
      <Collapsible open={open} onOpenChange={setOpen}>
        <div className='bg-card ring-foreground/10 rounded-lg ring-1'>
          <CollapsibleTrigger
            render={
              <button
                type='button'
                className='hover:bg-muted/40 flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-left transition-colors'
              />
            }
          >
            <span className='text-sm font-medium'>{t('Capture Evidence')}</span>
            <span className='text-muted-foreground truncate text-xs'>
              {setting.retention_days}d
            </span>
            <ChevronDown
              className={cn(
                'ms-auto size-3.5 shrink-0 transition-transform',
                open && 'rotate-180'
              )}
            />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className='space-y-3 border-t px-3 py-3'>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Captured data is deleted automatically after the retention period'
                )}
              </p>
              {usageQuery.data ? (
                <p className='text-xs'>
                  {t('{{count}} records, {{size}} in total', {
                    count: usageQuery.data.records,
                    size: formatBytes(usageQuery.data.bytes),
                  })}
                </p>
              ) : null}
              <div className='space-y-1'>
                <Label htmlFor='capture-retention-days' className='text-xs'>
                  {t('Retention days')}
                </Label>
                <Input
                  id='capture-retention-days'
                  type='number'
                  min={1}
                  max={3650}
                  className='h-8'
                  value={setting.retention_days}
                  onChange={(event) =>
                    setSetting((previous) => ({
                      ...previous,
                      retention_days: Number(event.target.value) || 0,
                    }))
                  }
                />
              </div>
              <div className='flex items-center justify-between gap-2'>
                <Button
                  size='sm'
                  onClick={() => saveMutation.mutate(setting)}
                  disabled={saveMutation.isPending}
                >
                  {t('Save')}
                </Button>
                <Button
                  variant='destructive'
                  size='sm'
                  className='gap-1.5'
                  disabled={clearAllMutation.isPending}
                  onClick={() => setClearAllOpen(true)}
                >
                  <Eraser className='size-4' />
                  {t('Clear all')}
                </Button>
              </div>
            </div>
          </CollapsibleContent>
        </div>
      </Collapsible>
      <ConfirmDialog
        open={clearAllOpen}
        onOpenChange={setClearAllOpen}
        title={t('Clear all captured data?')}
        desc={t(
          'Removes the saved request and response payloads of every log record. The log records themselves are kept'
        )}
        destructive
        isLoading={clearAllMutation.isPending}
        confirmText={t('Clear')}
        handleConfirm={() => clearAllMutation.mutate()}
      />
    </>
  )
}
