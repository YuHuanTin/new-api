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

import { getExceptionCapture, type ExceptionLogLocator } from '../../api'
import {
  parseExceptionLogOther,
  type ExceptionLog,
  type ExceptionLogAnomaly,
  type ExceptionLogOther,
} from '../../types'
import { getExceptionErrorInfo } from '../exception-logs-columns'

/** 把请求头拼成 "name: value" 的行，同名多值各占一行，跳过空表。 */
function headerLines(headers?: Record<string, string | string[]>): string[] {
  if (!headers) {
    return []
  }
  return Object.entries(headers).flatMap(([name, value]) =>
    Array.isArray(value)
      ? value.map((item) => `${name}: ${item}`)
      : [`${name}: ${value}`]
  )
}

/** 现场信息里只要有可读内容才展示报文框。 */
function hasExchange(anomaly?: ExceptionLogAnomaly): boolean {
  if (!anomaly) {
    return false
  }
  return Boolean(
    anomaly.request_url ||
    anomaly.request_body ||
    anomaly.upstream_body ||
    Object.keys(anomaly.request_headers ?? {}).length > 0 ||
    Object.keys(anomaly.upstream_headers ?? {}).length > 0
  )
}

const DISPLAY_BODY_MAX_BYTES = 4096

// 正文不超过 4KB 才直接展示，更大的只在下载文件里提供
function displayBody(body?: string): string | undefined {
  if (!body) {
    return undefined
  }
  return new TextEncoder().encode(body).length <= DISPLAY_BODY_MAX_BYTES
    ? body
    : undefined
}

/**
 * 把请求和上游响应拼成一段可读的报文：
 *
 * >>> POST https://host/path
 * header: value
 *
 * body
 *
 * <<< HTTP 400
 * header: value
 *
 * body
 */
function buildExchangeText(
  anomaly: ExceptionLogAnomaly,
  includeBody: boolean
): string {
  const requestLine = [anomaly.request_method, anomaly.request_url]
    .filter(Boolean)
    .join(' ')
  const requestHeaders = headerLines(anomaly.request_headers)
  const requestBody = includeBody
    ? anomaly.request_body
    : displayBody(anomaly.request_body)
  const lines: string[] = [`>>>${requestLine ? ` ${requestLine}` : ''}`]
  lines.push(...requestHeaders)
  if (requestBody) {
    lines.push('')
    lines.push(requestBody)
  }
  if (requestHeaders.length > 0 || requestBody) {
    lines.push('')
  }

  lines.push(
    anomaly.upstream_status ? `<<< HTTP ${anomaly.upstream_status}` : '<<<'
  )
  const responseHeaders = headerLines(anomaly.upstream_headers)
  lines.push(...responseHeaders)
  const responseBody = includeBody
    ? anomaly.upstream_body
    : displayBody(anomaly.upstream_body)
  if (responseBody) {
    lines.push('')
    lines.push(responseBody)
  }

  return lines.join('\n')
}

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

/** 报文框。现场数据要么来自列表行（旧数据 / 测试），要么按需从接口拉取。 */
function ExchangeBlock({ anomaly }: { anomaly: ExceptionLogAnomaly }) {
  const { t } = useTranslation()
  const download = () => {
    const blob = new Blob([buildExchangeText(anomaly, true)], {
      type: 'text/plain;charset=utf-8',
    })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `http-capture-${Date.now()}.txt`
    anchor.click()
    URL.revokeObjectURL(url)
  }
  return (
    <DetailSection label={t('Request and upstream response')}>
      <div className='space-y-2'>
        <div className='flex items-center justify-end gap-2'>
          <CopyButton
            value={buildExchangeText(anomaly, false)}
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
          {buildExchangeText(anomaly, false)}
        </pre>
      </div>
    </DetailSection>
  )
}

/** 列表接口不再返回抓包现场，打开详情时才按日志定位信息拉取。 */
function RemoteExchange({
  locator,
  open,
}: {
  locator: ExceptionLogLocator
  open: boolean
}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: [
      'exception-capture',
      locator.id,
      locator.request_id,
      locator.created_at,
    ],
    queryFn: () => getExceptionCapture(locator),
    enabled: open,
  })
  if (query.isPending) {
    return <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
  }
  if (!query.data || !hasExchange(query.data)) {
    return null
  }
  return <ExchangeBlock anomaly={query.data} />
}

export function ExceptionDetailDialog(props: {
  log: ExceptionLog | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
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

        {hasExchange(anomaly) ? (
          <ExchangeBlock anomaly={anomaly as ExceptionLogAnomaly} />
        ) : (
          <RemoteExchange
            locator={{
              id: props.log.id,
              request_id: props.log.request_id,
              created_at: props.log.created_at,
            }}
            open={props.open}
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
