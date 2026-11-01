import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
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
  vi.unstubAllGlobals()
  Reflect.deleteProperty(navigator, 'clipboard')
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
  it('shows request and response headers with bodies up to 4KB', () => {
    renderDialog(
      makeLog({
        request_path: '/v1/chat/completions',
        error_code: 'cyber_policy',
        admin_info: {
          anomaly: {
            request_method: 'POST',
            request_url: 'https://api.example.com/v1/chat/completions',
            request_headers: {
              'content-type': ['application/json'],
              authorization: ['Bearer a', 'Bearer b'],
            },
            request_body: '{"model":"gpt-5.1"}',
            upstream_status: 400,
            upstream_headers: {
              'set-cookie': ['a=1', 'b=2'],
              'x-request-id': ['up-1'],
            },
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
    expect(
      text.startsWith('>>> POST https://api.example.com/v1/chat/completions')
    ).toBe(true)
    expect(text).toContain(`content-type: application/json
authorization: Bearer a
authorization: Bearer b

{"model":"gpt-5.1"}

<<< HTTP 400`)
    expect(text).toContain(`set-cookie: a=1
set-cookie: b=2
x-request-id: up-1

{"error":{"code":"cyber_policy"}}`)
    expect(screen.getByRole('button', { name: 'Download' })).toBeVisible()
  })

  it('copies exactly the text shown in the exchange block', async () => {
    const writeText = vi.fn<(text: string) => Promise<void>>()
    writeText.mockResolvedValue()
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })

    renderDialog(
      makeLog({
        admin_info: {
          anomaly: {
            request_method: 'POST',
            request_url: 'https://api.example.com/v1/chat/completions',
            request_headers: { 'content-type': ['application/json'] },
            request_body: '{"model":"gpt-5.1"}',
            upstream_status: 400,
            upstream_body: '{"error":{"code":"cyber_policy"}}',
          },
        },
      })
    )

    const block = screen.getByText((_, node) => {
      if (node?.tagName !== 'PRE') return false
      return node.textContent?.startsWith('>>>') ?? false
    })
    fireEvent.click(screen.getByRole('button', { name: 'Copy to clipboard' }))
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(block.textContent)
    )
  })

  it('renders headers stored as single strings by older captures', () => {
    renderDialog(
      makeLog({
        admin_info: {
          anomaly: {
            request_method: 'POST',
            request_url: 'https://api.example.com/v1/chat/completions',
            request_headers: { 'content-type': 'application/json' },
            upstream_status: 500,
            upstream_headers: { 'x-request-id': 'up-1' },
          },
        },
      })
    )

    const block = screen.getByText((_, node) => {
      if (node?.tagName !== 'PRE') return false
      return node.textContent?.startsWith('>>>') ?? false
    })
    expect(block.textContent).toContain('content-type: application/json')
    expect(block.textContent).toContain('x-request-id: up-1')
  })

  it('downloads the full raw HTTP including bodies', async () => {
    const createObjectURL = vi.fn<(blob: Blob) => string>(() => 'blob:mock')
    const revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => undefined)

    renderDialog(
      makeLog({
        admin_info: {
          anomaly: {
            request_method: 'POST',
            request_url: 'https://api.example.com/v1/chat/completions',
            request_headers: {
              'content-type': ['application/json'],
              'x-trace': ['a', 'b'],
            },
            request_body: '{"model":"gpt-5.1"}',
            upstream_status: 400,
            upstream_headers: { 'x-request-id': ['up-1'] },
            upstream_body: '{"error":{"code":"cyber_policy"}}',
          },
        },
      })
    )

    fireEvent.click(screen.getByRole('button', { name: 'Download' }))

    expect(createObjectURL).toHaveBeenCalledTimes(1)
    const blob = createObjectURL.mock.calls[0][0] as Blob
    const text = await blob.text()
    expect(text.startsWith('>>> POST ')).toBe(true)
    expect(text).toContain(`{"model":"gpt-5.1"}

<<< HTTP 400`)
    expect(text).toContain(`x-request-id: up-1

{"error":{"code":"cyber_policy"}}`)
    expect(click).toHaveBeenCalled()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock')
  })

  it('hides bodies larger than 4096 bytes but keeps them in the download', async () => {
    const bigBody = 'A'.repeat(4097)
    const createObjectURL = vi.fn<(blob: Blob) => string>(() => 'blob:mock')
    const revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
      () => undefined
    )

    renderDialog(
      makeLog({
        admin_info: {
          anomaly: {
            request_method: 'POST',
            request_url: 'https://api.example.com/v1/chat/completions',
            request_body: bigBody,
            upstream_status: 500,
            upstream_body: bigBody,
          },
        },
      })
    )

    const block = screen.getByText((_, node) => {
      if (node?.tagName !== 'PRE') return false
      return node.textContent?.startsWith('>>>') ?? false
    })
    expect(block.textContent).not.toContain(bigBody.slice(0, 32))

    fireEvent.click(screen.getByRole('button', { name: 'Download' }))
    const blob = createObjectURL.mock.calls[0][0] as Blob
    const text = await blob.text()
    expect(text).toContain(bigBody)
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
      params: { id: 1, request_id: 'req-1', created_at: 1 },
    })
  })

  it('omits the status line and body when the upstream status is unknown', () => {
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
    expect(block.textContent).toContain('<<<')
    expect(block.textContent).not.toContain('<<< HTTP')
    expect(block.textContent).toContain('{"type":"response.failed"}')
  })
})
