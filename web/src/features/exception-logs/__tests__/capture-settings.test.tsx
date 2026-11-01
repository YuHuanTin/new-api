import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { CaptureSettings } from '../components/capture-settings'

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CaptureSettings />
    </QueryClientProvider>
  )
}

// 设置和用量是两个接口，按 URL 区分返回
function mockNetwork(options: Record<string, string> = {}) {
  return vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/exception/capture_size'
          ? { records: 2, bytes: 2048 }
          : { options },
    },
  }))
}

afterEach(() => {
  vi.restoreAllMocks()
})

test('loads and saves the capture retention days', async () => {
  const user = userEvent.setup()
  mockNetwork({ 'exception_setting.capture_retention_days': '7' })
  const patch = vi
    .spyOn(api, 'patch')
    .mockResolvedValue({ data: { success: true, data: { options: {} } } })

  renderPanel()
  await user.click(screen.getByRole('button', { name: /Capture Evidence/ }))

  const input = await screen.findByLabelText('Retention days')
  await waitFor(() => expect(input).toHaveValue(7))

  await user.clear(input)
  await user.type(input, '5')
  await user.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() =>
    expect(patch).toHaveBeenCalledWith('/api/exception/capture_setting', {
      options: { 'exception_setting.capture_retention_days': '5' },
    })
  )
})

test('shows the total capture records and size', async () => {
  const user = userEvent.setup()
  mockNetwork()

  renderPanel()
  await user.click(screen.getByRole('button', { name: /Capture Evidence/ }))

  expect(await screen.findByText('2 records, 2.0 KB in total')).toBeVisible()
})

test('clears every saved capture after confirmation', async () => {
  const user = userEvent.setup()
  mockNetwork()
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: { cleared: 3 } } })

  renderPanel()
  await user.click(screen.getByRole('button', { name: /Capture Evidence/ }))
  await user.click(await screen.findByRole('button', { name: 'Clear all' }))
  expect(
    screen.getByText(
      'Removes the saved request and response payloads of every log record. The log records themselves are kept'
    )
  ).toBeVisible()

  await user.click(screen.getByRole('button', { name: 'Clear' }))

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/exception/clear_capture', {
      all: true,
    })
  )
})

test('does not clear captures when the confirmation is cancelled', async () => {
  const user = userEvent.setup()
  mockNetwork()
  const post = vi.spyOn(api, 'post')

  renderPanel()
  await user.click(screen.getByRole('button', { name: /Capture Evidence/ }))
  await user.click(await screen.findByRole('button', { name: 'Clear all' }))
  await user.click(screen.getByRole('button', { name: 'Cancel' }))

  expect(post).not.toHaveBeenCalled()
})
