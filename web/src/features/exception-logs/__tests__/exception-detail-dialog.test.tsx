import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { api } from '@/lib/api'

import { ExceptionDetailDialog } from '../components/dialogs/exception-detail-dialog'
import type { ExceptionLog } from '../types'

vi.hoisted(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
})

const i18n = createInstance()
void i18n.init({
  lng: 'en',
  resources: { en: { translation: en.translation } },
})

afterEach(() => {
  vi.restoreAllMocks()
})

function makeLog(other: unknown): ExceptionLog {
  return {
    id: 1,
    created_at: 1,
    type: 5,
    user_id: 1,
    username: 'alice',
    token_name: 'token',
    model_name: 'gpt-5.1',
    channel_id: 1,
    channel_name: 'sub2api',
    content: 'status_code=400, blocked',
    use_time: 1,
    is_stream: false,
    request_id: 'req-1',
    upstream_request_id: '',
    other: JSON.stringify(other),
  }
}

function renderDialog(log: ExceptionLog) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ExceptionDetailDialog log={log} open onOpenChange={() => undefined} />
    </I18nextProvider>
  )
}

describe('exception detail dialog', () => {
  it('shows the request and the upstream response in one block', () => {
    renderDialog(
      makeLog({
        request_path: '/v1/chat/completions',
        error_code: 'cyber_policy',
        admin_info: {
          anomaly: {
            request_method: 'POST',
            request_url: 'https://api.example.com/v1/chat/completions',
            request_headers: { 'content-type': 'application/json' },
            request_body: '{"model":"gpt-5.1"}',
            upstream_status: 400,
            upstream_headers: { 'x-request-id': 'up-1' },
            upstream_body: '{"error":{"code":"cyber_policy"}}',
          },
        },
      })
    )

    const block = screen.getByText((_, node) => {
      if (node?.tagName !== 'PRE') return false
      return node.textContent?.startsWith('>>>') ?? false
    })
    const text = block.textContent ?? ''
    expect(text).toContain('POST https://api.example.com/v1/chat/completions')
    expect(text).toContain('content-type: application/json')
    expect(text).toContain('{"model":"gpt-5.1"}')
    expect(text).toContain('<<< HTTP 400')
    expect(text).toContain('x-request-id: up-1')
    expect(text).toContain('{"error":{"code":"cyber_policy"}}')
  })

  it('loads the capture on demand when the list does not carry it', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        success: true,
        data: {
          anomaly: {
            request_method: 'POST',
            request_url: 'https://up.example.com/v1/chat/completions',
            upstream_status: 502,
            upstream_body: 'bad gateway',
          },
        },
      },
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <ExceptionDetailDialog
            log={makeLog({ error_code: 'upstream_error' })}
            open
            onOpenChange={() => undefined}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    const block = await screen.findByText((_, node) => {
      if (node?.tagName !== 'PRE') return false
      return node.textContent?.startsWith('>>>') ?? false
    })
    expect(block.textContent).toContain(
      'POST https://up.example.com/v1/chat/completions'
    )
    expect(block.textContent).toContain('<<< HTTP 502')
    expect(block.textContent).toContain('bad gateway')
    expect(api.get).toHaveBeenCalledWith('/api/exception/capture', {
      params: { id: 1 },
    })
  })

  it('omits the status line when the upstream status is unknown', () => {
    renderDialog(
      makeLog({
        error_code: 'cyber_policy',
        admin_info: {
          anomaly: {
            request_method: 'POST',
            request_url: 'https://api.example.com/v1/responses',
            upstream_body: '{"type":"response.failed"}',
          },
        },
      })
    )

    const block = screen.getByText((_, node) => {
      if (node?.tagName !== 'PRE') return false
      return node.textContent?.startsWith('>>>') ?? false
    })
    expect(block.textContent).toContain('<<<\n{"type":"response.failed"}')
  })
})
