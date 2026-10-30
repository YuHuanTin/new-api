import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { CyberGuardSettings } from '../components/cyber-guard-settings'

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CyberGuardSettings />
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

// 已开启计数的渠道要能看到并一键关闭
test('lists channels with cyber counting and turns one off', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/exception/cyber_guard') {
      return { data: { success: true, data: { options: {} } } }
    }
    if (url === '/api/exception/cyber_guard/channels') {
      return {
        data: {
          success: true,
          data: { items: [{ id: 7, name: 'chan-7', status: 1 }] },
        },
      }
    }
    return { data: { success: true, data: {} } }
  })
  const patch = vi
    .spyOn(api, 'patch')
    .mockResolvedValue({ data: { success: true } })

  renderPanel()
  await user.click(screen.getByRole('button', { name: /Cyber Policy Control/ }))
  expect(await screen.findByText('chan-7')).toBeVisible()

  const toggle = screen.getByRole('switch', { name: /chan-7/ })
  await user.click(toggle)

  await waitFor(() =>
    expect(patch).toHaveBeenCalledWith('/api/exception/cyber_guard/channel', {
      id: 7,
      enabled: false,
    })
  )
  // 惰性 UI：关掉后行还留着，可以马上再打开
  expect(screen.getByText('chan-7')).toBeVisible()

  await user.click(toggle)
  await waitFor(() =>
    expect(patch).toHaveBeenLastCalledWith(
      '/api/exception/cyber_guard/channel',
      { id: 7, enabled: true }
    )
  )
})

test('says so when no channel counts rejections', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { options: {}, items: [] } },
  })
  renderPanel()
  await user.click(screen.getByRole('button', { name: /Cyber Policy Control/ }))
  expect(
    await screen.findByText('No channel has cyber policy counting enabled')
  ).toBeVisible()
})
