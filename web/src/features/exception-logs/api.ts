import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  CyberGuardChannel,
  CyberGuardSetting,
  ExceptionLogAnomaly,
  ExceptionLogsResult,
} from './types'

export interface ExceptionLogsParams {
  page: number
  pageSize: number
  username?: string
  errorCode?: string
}

/** 拉取失败的请求：错误日志行，以及流状态判定为失败的消耗日志行。 */
export async function getExceptionLogs(params: ExceptionLogsParams) {
  const response = await api.get('/api/log/', {
    params: {
      p: params.page,
      page_size: params.pageSize,
      failed_only: 1,
      username: params.username || undefined,
      error_code: params.errorCode || undefined,
    },
  })
  return requireServerSuccess(response.data).data as ExceptionLogsResult
}

/** 清掉选中请求保存的抓包现场（请求/响应报文），日志记录本身不动。 */
export async function clearExceptionCapture(ids: number[]) {
  const response = await api.post('/api/exception/clear_capture', { ids })
  return requireServerSuccess(response.data).data as { cleared: number }
}

/** 打开异常详情时才拉取抓包现场；列表接口不带这块数据。 */
export async function getExceptionCapture(id: number) {
  const response = await api.get('/api/exception/capture', { params: { id } })
  const data = requireServerSuccess(response.data).data as {
    anomaly?: ExceptionLogAnomaly
  }
  return data.anomaly
}

export async function getCyberGuardSetting() {
  const response = await api.get('/api/exception/cyber_guard')
  return requireServerSuccess(response.data).data as {
    options: Record<string, string>
  }
}

export async function getCyberGuardChannels() {
  const response = await api.get('/api/exception/cyber_guard/channels')
  const data = requireServerSuccess(response.data).data as {
    items: CyberGuardChannel[]
  }
  return data.items
}

export async function setChannelCyberGuard(id: number, enabled: boolean) {
  const response = await api.patch('/api/exception/cyber_guard/channel', {
    id,
    enabled,
  })
  return requireServerSuccess(response.data).data
}

export async function saveCyberGuardSetting(options: Record<string, string>) {
  const response = await api.patch('/api/exception/cyber_guard', { options })
  return requireServerSuccess(response.data).data as {
    options: Record<string, string>
  }
}

/** 把后端返回的选项字符串读成设置对象。 */
export function readCyberGuardSetting(
  options: Record<string, string>
): CyberGuardSetting {
  return {
    auto_ban: options['cyber_setting.auto_ban'] !== 'false',
    window_minutes: Number(options['cyber_setting.window_minutes'] ?? 10) || 10,
    threshold: Number(options['cyber_setting.threshold'] ?? 3) || 3,
  }
}

/** 把设置对象写回后端认识的选项字符串。 */
export function writeCyberGuardSetting(
  setting: CyberGuardSetting
): Record<string, string> {
  return {
    'cyber_setting.auto_ban': String(setting.auto_ban),
    'cyber_setting.window_minutes': String(setting.window_minutes),
    'cyber_setting.threshold': String(setting.threshold),
  }
}
