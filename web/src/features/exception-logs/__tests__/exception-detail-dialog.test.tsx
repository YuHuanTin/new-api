import { Blob as NodeBlob } from 'node:buffer'
import { DecompressionStream as NodeDecompressionStream } from 'node:stream/web'
import { gzipSync } from 'node:zlib'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import en from '@/i18n/locales/en.json'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

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

beforeEach(() => {
  vi.stubGlobal('Blob', NodeBlob)
  vi.stubGlobal('DecompressionStream', NodeDecompressionStream)
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'root', role: ROLE.SUPER_ADMIN })
})

afterEach(() => {
  useAuthStore.setState(useAuthStore.getInitialState(), true)
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

function renderDialog(
  log: ExceptionLog,
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
) {
  const element = (open: boolean) => (
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <ExceptionDetailDialog
          log={log}
          open={open}
          onOpenChange={() => undefined}
        />
      </I18nextProvider>
    </QueryClientProvider>
  )
  const view = render(element(true))
  return {
    ...view,
    setOpen: (open: boolean) => view.rerender(element(open)),
    client,
  }
}

const raw =
  '>>> POST https://upstream/v1/responses\nAuthorization: Bearer upstream-secret\nX-Multi: one\nX-Multi: two\n\n{"model":"test"}\n\n<<< HTTP 502\nSet-Cookie: session=secret\n\n{"tool_calls":[{"id":"tool-1"}]}\n{"error":"partial-a'

function compress(value: string | Uint8Array): string {
  return `gz:${gzipSync(value).toString('base64')}`
}

async function captureBlock(): Promise<HTMLElement> {
  return screen.findByText((_, node) => node?.tagName === 'PRE')
}

describe('exception detail dialog', () => {
  it('decompresses and displays raw content without rebuilding headers or JSON', async () => {
    renderDialog(makeLog({ admin_info: { anomaly: compress(raw) } }))
    expect((await captureBlock()).textContent).toBe(raw)
    expect(screen.getByRole('button', { name: 'Download' })).toBeVisible()
  })

  it('copies the full decompressed raw string', async () => {
    const writeText = vi
      .fn<(text: string) => Promise<void>>()
      .mockResolvedValue()
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })
    renderDialog(makeLog({ admin_info: { anomaly: compress(raw) } }))
    await captureBlock()
    fireEvent.click(screen.getByRole('button', { name: 'Copy to clipboard' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(raw))
  })

  it('downloads all original bytes, including binary bytes beyond the preview', async () => {
    const bytes = new Uint8Array([
      ...new TextEncoder().encode(raw + 'x'.repeat(65536)),
      255,
      0,
    ])
    const createObjectURL = vi.fn<(blob: Blob | MediaSource) => string>(
      () => 'blob:mock'
    )
    vi.spyOn(URL, 'createObjectURL').mockImplementation(createObjectURL)
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
      () => undefined
    )
    renderDialog(makeLog({ admin_info: { anomaly: compress(bytes) } }))
    expect((await captureBlock()).textContent).not.toContain('x'.repeat(4097))
    fireEvent.click(screen.getByRole('button', { name: 'Download' }))
    const blob = createObjectURL.mock.calls[0][0] as Blob
    expect(new Uint8Array(await blob.arrayBuffer())).toEqual(bytes)
  })

  it.each([
    {
      request: 'r'.repeat(4096),
      response: 's'.repeat(4096),
      hideRequest: false,
      hideResponse: false,
    },
    {
      request: 'r'.repeat(4097),
      response: 'small response',
      hideRequest: true,
      hideResponse: false,
    },
    {
      request: 'small request',
      response: 's'.repeat(4097),
      hideRequest: false,
      hideResponse: true,
    },
    {
      request: '界'.repeat(1366),
      response: 's'.repeat(4097),
      hideRequest: true,
      hideResponse: true,
    },
  ])(
    'hides each body independently above 4 KiB ($hideRequest/$hideResponse)',
    async ({ request, response, hideRequest, hideResponse }) => {
      const wire = `>>> POST https://upstream\nX-Request: original\n\n${request}\n\n<<< HTTP 502\nX-Response: original\n\n${response}\n`
      const writeText = vi
        .fn<(text: string) => Promise<void>>()
        .mockResolvedValue()
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText },
        configurable: true,
      })
      renderDialog(makeLog({ admin_info: { anomaly: compress(wire) } }))
      const preview = (await captureBlock()).textContent ?? ''
      expect(preview).toContain('X-Request: original')
      expect(preview).toContain('X-Response: original')
      expect(preview.includes(request)).toBe(!hideRequest)
      expect(preview.includes(response)).toBe(!hideResponse)
      expect(preview.split('(omited...)').length - 1).toBe(
        Number(hideRequest) + Number(hideResponse)
      )
      fireEvent.click(screen.getByRole('button', { name: 'Copy to clipboard' }))
      await waitFor(() => expect(writeText).toHaveBeenCalledWith(preview))
    }
  )

  it.each([false, true])(
    'keeps old object records readable (compressed=%s)',
    async (compressed) => {
      const legacy = {
        request_headers: { Authorization: 'Bearer old-secret' },
        upstream_body: 'old response',
      }
      const encoded = compressed ? compress(JSON.stringify(legacy)) : legacy
      renderDialog(makeLog({ admin_info: { anomaly: encoded } }))
      expect((await captureBlock()).textContent).toBe(JSON.stringify(legacy))
    }
  )

  it.each([ROLE.ADMIN, ROLE.USER])(
    'does not fetch or expose captures for role %s',
    (role) => {
      useAuthStore.getState().auth.setUser({ id: 2, username: 'viewer', role })
      const get = vi.spyOn(api, 'get')
      renderDialog(makeLog({ admin_info: { anomaly: compress(raw) } }))
      expect(screen.queryByText(/upstream-secret/)).toBeNull()
      expect(screen.queryByRole('button', { name: 'Download' })).toBeNull()
      expect(get).not.toHaveBeenCalled()
    }
  )

  it('reuses cached decoded details on reopen and refreshes after capture clearing', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { anomaly: compress(raw) } },
    })
    const view = renderDialog(makeLog({ error_code: 'upstream_error' }))
    expect((await captureBlock()).textContent).toBe(raw)
    expect(get).toHaveBeenCalledWith('/api/exception/capture', {
      params: { id: 1, request_id: 'req-1', created_at: 1 },
    })
    view.setOpen(false)
    view.setOpen(true)
    expect((await captureBlock()).textContent).toBe(raw)
    expect(get).toHaveBeenCalledTimes(1)
    get.mockResolvedValue({ data: { success: true, data: { anomaly: '' } } })
    await view.client.invalidateQueries({ queryKey: ['exception-capture'] })
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: 'Download' })).toBeNull()
    )
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('does not reuse another root account capture cache', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { anomaly: compress(raw) } },
    })
    const view = renderDialog(makeLog({}))
    await captureBlock()
    view.unmount()
    useAuthStore
      .getState()
      .auth.setUser({ id: 3, username: 'other-root', role: ROLE.SUPER_ADMIN })
    get.mockResolvedValue({
      data: {
        success: true,
        data: { anomaly: compress('other account content') },
      },
    })
    renderDialog(makeLog({}), view.client)
    expect((await captureBlock()).textContent).toBe('other account content')
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('shows a decoding failure instead of an empty capture', async () => {
    renderDialog(makeLog({ admin_info: { anomaly: 'gz:broken' } }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Request failed')
    expect(screen.queryByRole('button', { name: 'Download' })).toBeNull()
  })
})
