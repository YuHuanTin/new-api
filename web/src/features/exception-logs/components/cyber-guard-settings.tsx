import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import {
  getCyberGuardChannels,
  getCyberGuardSetting,
  readCyberGuardSetting,
  saveCyberGuardSetting,
  setChannelCyberGuard,
  writeCyberGuardSetting,
} from '../api'
import type { CyberGuardSetting } from '../types'

const DEFAULT_SETTING: CyberGuardSetting = {
  auto_ban: true,
  window_minutes: 10,
  threshold: 3,
}

/**
 * cyber 策略管控的共用参数。开关本身在渠道上（渠道 → 编辑 → 高级设置），
 * 默认全关，所以这里没有「总开关」
 */
export function CyberGuardSettings() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [setting, setSetting] = useState<CyberGuardSetting>(DEFAULT_SETTING)

  const { data } = useQuery({
    queryKey: ['cyber-guard-setting'],
    queryFn: async () => {
      const result = await getCyberGuardSetting()
      return readCyberGuardSetting(result.options)
    },
  })

  useEffect(() => {
    if (data) {
      setSetting(data)
    }
  }, [data])

  const channelsQuery = useQuery({
    queryKey: ['cyber-guard-channels'],
    queryFn: getCyberGuardChannels,
  })

  // 关掉后先把行留在界面上，方便立刻再打开；刷新后列表只返回仍开启的渠道
  const [channelStates, setChannelStates] = useState<Record<number, boolean>>(
    {}
  )
  const toggleChannelMutation = useMutation({
    mutationFn: (variables: { id: number; enabled: boolean }) =>
      setChannelCyberGuard(variables.id, variables.enabled),
    onSuccess: (_result, variables) => {
      setChannelStates((previous) => ({
        ...previous,
        [variables.id]: variables.enabled,
      }))
      toast.success(
        variables.enabled
          ? t('Cyber counting turned on')
          : t('Cyber counting turned off')
      )
    },
    onError: (error) => {
      handleServerError(error, t('Failed to update channel'))
    },
  })

  const saveMutation = useMutation({
    mutationFn: async (next: CyberGuardSetting) =>
      saveCyberGuardSetting(writeCyberGuardSetting(next)),
    onSuccess: async () => {
      toast.success(t('Cyber guard setting saved'))
      await queryClient.invalidateQueries({ queryKey: ['cyber-guard-setting'] })
    },
    onError: (error) => {
      handleServerError(error, t('Failed to save cyber guard setting'))
    },
  })

  return (
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
          <span className='text-sm font-medium'>
            {t('Cyber Policy Control')}
          </span>
          <span className='text-muted-foreground truncate text-xs'>
            {t('Enabled per channel')} · {setting.window_minutes}m /{' '}
            {setting.threshold}
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
                'Counting is off for every channel by default. Turn it on for a channel in its settings; only rejections from channels that enable it are counted'
              )}
            </p>
            <div className='space-y-1.5'>
              <p className='text-xs font-medium'>
                {t('Channels counting rejections')}
              </p>
              {(channelsQuery.data ?? []).length === 0 ? (
                <p className='text-muted-foreground text-xs'>
                  {t('No channel has cyber policy counting enabled')}
                </p>
              ) : (
                (channelsQuery.data ?? []).map((channel) => {
                  const switchId = `cyber-guard-channel-${channel.id}`
                  return (
                    <div
                      key={channel.id}
                      className='flex items-center justify-between gap-3 rounded-md border px-2.5 py-1.5'
                    >
                      <Label htmlFor={switchId} className='min-w-0 text-xs'>
                        <span className='truncate'>{channel.name}</span>
                        <span className='text-muted-foreground'>
                          {' '}
                          #{channel.id}
                        </span>
                      </Label>
                      <Switch
                        id={switchId}
                        checked={channelStates[channel.id] ?? true}
                        disabled={toggleChannelMutation.isPending}
                        onCheckedChange={(checked) =>
                          toggleChannelMutation.mutate({
                            id: channel.id,
                            enabled: checked,
                          })
                        }
                      />
                    </div>
                  )
                })
              )}
            </div>
            <div className='flex items-center justify-between gap-4'>
              <Label htmlFor='cyber-guard-auto-ban' className='text-xs'>
                {t('Disable the account when the threshold is exceeded')}
              </Label>
              <Switch
                id='cyber-guard-auto-ban'
                checked={setting.auto_ban}
                onCheckedChange={(checked) =>
                  setSetting((previous) => ({ ...previous, auto_ban: checked }))
                }
              />
            </div>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1'>
                <Label htmlFor='cyber-guard-window' className='text-xs'>
                  {t('Counting window (minutes)')}
                </Label>
                <Input
                  id='cyber-guard-window'
                  type='number'
                  min={1}
                  max={1440}
                  className='h-8'
                  value={setting.window_minutes}
                  onChange={(event) =>
                    setSetting((previous) => ({
                      ...previous,
                      window_minutes: Number(event.target.value) || 0,
                    }))
                  }
                />
              </div>
              <div className='space-y-1'>
                <Label htmlFor='cyber-guard-threshold' className='text-xs'>
                  {t('Rejections allowed in the window')}
                </Label>
                <Input
                  id='cyber-guard-threshold'
                  type='number'
                  min={1}
                  max={1000}
                  className='h-8'
                  value={setting.threshold}
                  onChange={(event) =>
                    setSetting((previous) => ({
                      ...previous,
                      threshold: Number(event.target.value) || 0,
                    }))
                  }
                />
              </div>
            </div>
            <Button
              size='sm'
              onClick={() => saveMutation.mutate(setting)}
              disabled={saveMutation.isPending}
            >
              {t('Save')}
            </Button>
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}
