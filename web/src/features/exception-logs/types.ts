import type { PolicyEvent } from '@/features/system-settings/request-policies/api'

export interface ExceptionLog {
  id: number
  created_at: number
  type: number
  user_id: number
  username: string
  token_name: string
  model_name: string
  channel_id: number
  channel_name: string
  content: string
  use_time: number
  is_stream: boolean
  request_id: string
  upstream_request_id: string
  other: string
}

export interface ExceptionLogStreamStatus {
  status?: string
  end_reason?: string
  error_code?: string
  error_type?: string
  error_count?: number
  end_error?: string
  errors?: string[]
}

export interface ExceptionLogAnomaly {
  request_method?: string
  request_url?: string
  request_body?: string
  request_headers?: Record<string, string>
  upstream_body?: string
  upstream_headers?: Record<string, string>
  upstream_status?: number
}

export interface ExceptionLogOther {
  request_path?: string
  error_type?: string
  error_code?: string
  status_code?: number
  stream_status?: ExceptionLogStreamStatus
  admin_info?: {
    anomaly?: ExceptionLogAnomaly
    use_channel?: string[]
    // 与「日志详情」的「请求策略决策」是同一份记录
    request_policy?: PolicyEvent[]
  }
}

export interface ExceptionLogsResult {
  page: number
  page_size: number
  total: number
  items: ExceptionLog[]
}

export interface CyberGuardChannel {
  id: number
  name: string
  status: number
}

export interface CyberGuardSetting {
  auto_ban: boolean
  window_minutes: number
  threshold: number
}

/** 解析日志里的 other 字段，解析失败时按空对象处理。 */
export function parseExceptionLogOther(
  other: string | undefined
): ExceptionLogOther {
  if (!other) {
    return {}
  }
  try {
    const parsed = JSON.parse(other) as unknown
    return parsed && typeof parsed === 'object'
      ? (parsed as ExceptionLogOther)
      : {}
  } catch {
    return {}
  }
}
