import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { PolicyDecisionRecord } from '@/features/system-settings/request-policies/decision-record'
import {
  DetailRow,
  DetailSection,
} from '@/features/usage-logs/components/dialogs/log-detail-layout'
import { formatTimestampToDate } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getExceptionCapture, type ExceptionLogLocator } from '../../api'
import {
  parseExceptionLogOther,
  type ExceptionLog,
  type ExceptionLogAnomaly,
  type ExceptionLogOther,
} from '../../types'
import { getExceptionErrorInfo } from '../exception-logs-columns'

function HeaderRows(props: { log: ExceptionLog; other: ExceptionLogOther }) {
  const { t } = useTranslation()
  const info = getExceptionErrorInfo(props.log)
  return (
    <div className='space-y-1.5'>
      <DetailRow
        label={t('Time')}
        value={formatTimestampToDate(props.log.created_at)}
      />
      <DetailRow label={t('User')} value={props.log.username || '-'} />
      <DetailRow label={t('Token')} value={props.log.token_name || '-'} />
      <DetailRow
        label={t('Channel')}
        value={props.log.channel_name || `#${props.log.channel_id || '-'}`}
      />
      <DetailRow label={t('Model')} value={props.log.model_name || '-'} />
      <DetailRow
        label={t('Request Path')}
        value={props.other.request_path || '-'}
      />
      <DetailRow label={t('Error Code')} value={info.code || '-'} mono />
      <DetailRow label={t('Error Type')} value={info.type || '-'} />
      {info.status ? (
        <DetailRow label={t('Status Code')} value={String(info.status)} />
      ) : null}
      <DetailRow
        label={t('Request ID')}
        value={props.log.request_id || '-'}
        mono
      />
      <DetailRow
        label={t('Upstream Request ID')}
        value={props.log.upstream_request_id || '-'}
        mono
      />
    </div>
  )
}

/** 分别按 UTF-8 字节数限制正文预览，null 在展示时替换为统一省略标记。 */
function capturePreview(raw: string): (string | null)[] {
  const encoder = new TextEncoder()
  const requestStart = raw.indexOf('\n\n') + 2
  const responses = [...raw.matchAll(/\n<<<(?: HTTP \d{3})?\n/g)]
  // 旧对象或正文内含报文分隔符时无法可靠分段，超限则只允许下载原文。
  if (!raw.startsWith('>>> ') || requestStart < 2 || responses.length !== 1) {
    return encoder.encode(raw).byteLength > 4096 ? [null] : [raw]
  }
  const responseStart = responses[0].index + 1
  const requestEnd = responseStart - 2
  const responseHeaderEnd = raw.indexOf('\n\n', responseStart)
  if (requestEnd < requestStart || responseHeaderEnd < 0) {
    return encoder.encode(raw).byteLength > 4096 ? [null] : [raw]
  }
  const responseBodyStart = responseHeaderEnd + 2
  const responseEnd = raw.endsWith('\n') ? raw.length - 1 : raw.length
  const request = raw.slice(requestStart, requestEnd)
  const response = raw.slice(responseBodyStart, responseEnd)
  return [
    raw.slice(0, requestStart),
    encoder.encode(request).byteLength > 4096 ? null : request,
    raw.slice(requestEnd, responseBodyStart),
    encoder.encode(response).byteLength > 4096 ? null : response,
    raw.slice(responseEnd),
  ]
}

/** 每个查询缓存只解压、计算预览一次，下载复用解压后的原始字节。 */
async function decodeCapture(encoded: string): Promise<{
  preview: (string | null)[]
  bytes: Uint8Array<ArrayBuffer>
}> {
  if (typeof encoded !== 'string' || !encoded.startsWith('gz:')) {
    const raw = typeof encoded === 'string' ? encoded : JSON.stringify(encoded)
    return {
      preview: capturePreview(raw),
      bytes: new TextEncoder().encode(raw),
    }
  }
  const bytes = Uint8Array.from(atob(encoded.slice(3)), (char) =>
    char.charCodeAt(0)
  )
  const stream = new Blob([bytes])
    .stream()
    .pipeThrough(new DecompressionStream('gzip'))
  const decoded = new Uint8Array(await new Response(stream).arrayBuffer())
  return {
    preview: capturePreview(new TextDecoder().decode(decoded)),
    bytes: decoded,
  }
}

/** 按需读取并解压，重新打开时复用按账号和会话隔离的缓存。 */
function RemoteExchange(props: {
  locator: ExceptionLogLocator
  open: boolean
  inline?: ExceptionLogAnomaly
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const sessionId = useAuthStore((state) => state.auth.session?.sid)
  const query = useQuery({
    queryKey: [
      'exception-capture',
      userId,
      sessionId,
      props.locator.id,
      props.locator.request_id,
      props.locator.created_at,
    ],
    queryFn: async () => {
      const encoded = props.inline ?? (await getExceptionCapture(props.locator))
      return encoded ? decodeCapture(encoded) : null
    },
    enabled: props.open,
    staleTime: Infinity,
    gcTime: 5 * 60 * 1000,
  })
  if (query.isPending) {
    return <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
  }
  if (query.isError) {
    return (
      <p role='alert' className='text-destructive text-sm'>
        {t('Request failed')}
      </p>
    )
  }
  if (!query.data) return null
  const capture = query.data
  const preview = capture.preview.map((part) => part ?? '(omited...)').join('')
  const download = (): void => {
    const url = URL.createObjectURL(
      new Blob([capture.bytes], { type: 'text/plain' })
    )
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `http-capture-${props.locator.id}.txt`
    anchor.click()
    URL.revokeObjectURL(url)
  }
  return (
    <DetailSection label={t('Request and upstream response')}>
      <div className='space-y-2'>
        <div className='flex items-center justify-end gap-2'>
          <CopyButton
            value={preview}
            variant='outline'
            size='sm'
            iconClassName='size-4'
            tooltip={t('Copy to clipboard')}
          />
          <Button size='sm' variant='outline' onClick={download}>
            {t('Download')}
          </Button>
        </div>
        <pre className='bg-background/60 max-h-96 overflow-auto rounded border p-2 font-mono text-[11px] leading-relaxed wrap-break-word whitespace-pre-wrap'>
          {preview}
        </pre>
      </div>
    </DetailSection>
  )
}

export function ExceptionDetailDialog(props: {
  log: ExceptionLog | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const isRoot = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  if (!props.log) {
    return null
  }
  const other = parseExceptionLogOther(props.log.other)
  const anomaly = other.admin_info?.anomaly
  const policy = other.admin_info?.request_policy ?? []

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Exception Details')}
      description={t(
        'Original request, upstream response and status of the failed request'
      )}
      contentClassName='sm:max-w-3xl'
    >
      <div className='space-y-4'>
        <DetailSection label={t('Failure')} variant='danger'>
          <div className='space-y-1.5'>
            <DetailRow label={t('Reason')} value={props.log.content || '-'} />
          </div>
        </DetailSection>

        <DetailSection label={t('Request')}>
          <HeaderRows log={props.log} other={other} />
        </DetailSection>

        {isRoot && (
          <RemoteExchange
            locator={{
              id: props.log.id,
              request_id: props.log.request_id,
              created_at: props.log.created_at,
            }}
            open={props.open}
            inline={anomaly}
          />
        )}

        {policy.length > 0 ? (
          <DetailSection label={t('Request policy decisions')}>
            <PolicyDecisionRecord
              events={policy}
              channelNames={
                props.log.channel_id
                  ? { [props.log.channel_id]: props.log.channel_name }
                  : undefined
              }
            />
          </DetailSection>
        ) : null}
      </div>
    </Dialog>
  )
}
