/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { afterEach, expect, test, vi } from 'vitest'

import { getAllLogs, getUserLogs } from '../../api'
import type { UsageLog } from '../../data/schema'
import { exportUsageLogs } from '../export'

vi.mock('../../api', () => ({
  getAllLogs: vi.fn(),
  getUserLogs: vi.fn(),
}))

const logs: UsageLog[] = [
  {
    id: 1,
    user_id: 7,
    created_at: 1,
    type: 2,
    content: 'first',
    username: 'user',
    token_name: 'token',
    model_name: 'model',
    quota: 1,
    prompt_tokens: 2,
    completion_tokens: 3,
    use_time: 4,
    is_stream: false,
    channel: 5,
    channel_name: '',
    token_id: 6,
    group: 'default',
    ip: '',
    other: '{}',
    request_id: 'request/1',
    upstream_request_id: '',
  },
  {
    id: 2,
    user_id: 7,
    created_at: 2,
    type: 2,
    content: 'second',
    username: 'user',
    token_name: 'token',
    model_name: 'model',
    quota: 1,
    prompt_tokens: 2,
    completion_tokens: 3,
    use_time: 4,
    is_stream: false,
    channel: 5,
    channel_name: '',
    token_id: 6,
    group: 'default',
    ip: '',
    other: '{}',
    request_id: 'request/2',
    upstream_request_id: '',
  },
]

afterEach(() => {
  vi.clearAllMocks()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

test('exports every filtered page through the user-scoped endpoint', async () => {
  const firstPage = Array.from({ length: 100 }, (_, index) => ({
    ...logs[0],
    id: index + 1,
    request_id: `request/${index + 1}`,
  }))
  vi.mocked(getUserLogs)
    .mockResolvedValueOnce({
      success: true,
      data: { items: firstPage, total: 101, page: 1, page_size: 100 },
    })
    .mockResolvedValueOnce({
      success: true,
      data: { items: logs.slice(1), total: 101, page: 2, page_size: 100 },
    })
  const objectUrl = 'blob:usage-logs'
  let exportedBlob: Blob | undefined
  const createObjectURL = vi.fn((blob: Blob) => {
    exportedBlob = blob
    return objectUrl
  })
  const revokeObjectURL = vi.fn()
  vi.stubGlobal(
    'URL',
    Object.assign(class extends URL {}, { createObjectURL, revokeObjectURL })
  )
  vi.spyOn(Date, 'now').mockReturnValue(123)
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
    () => undefined
  )
  const anchor = document.createElement('a')
  vi.spyOn(document, 'createElement').mockReturnValue(anchor)

  await exportUsageLogs({
    isAdmin: false,
    searchParams: {
      model: 'model',
      username: 'other-user',
      channel: '99',
    },
  })

  expect(getAllLogs).not.toHaveBeenCalled()
  expect(getUserLogs).toHaveBeenCalledTimes(2)
  expect(getUserLogs).toHaveBeenNthCalledWith(
    1,
    expect.objectContaining({ p: 1, page_size: 100, model_name: 'model' })
  )
  const firstRequest = vi.mocked(getUserLogs).mock.calls[0][0]
  expect(firstRequest).not.toHaveProperty('username')
  expect(firstRequest).not.toHaveProperty('channel')
  expect(anchor.download).toBe('usage-logs-123.json')
  if (!exportedBlob) throw new Error('The exported blob must be created')
  const exportedText = await exportedBlob.text()
  expect(exportedText).toContain('request/1')
  expect(exportedText).toContain('request/2')
  expect(revokeObjectURL).toHaveBeenCalledWith(objectUrl)
})

test('uses the admin endpoint for admin exports', async () => {
  vi.mocked(getAllLogs).mockResolvedValueOnce({
    success: true,
    data: { items: [], total: 0, page: 1, page_size: 100 },
  })
  const objectUrl = 'blob:usage-logs'
  const createObjectURL = vi.fn(() => objectUrl)
  const revokeObjectURL = vi.fn()
  vi.stubGlobal(
    'URL',
    Object.assign(class extends URL {}, { createObjectURL, revokeObjectURL })
  )
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
    () => undefined
  )

  await exportUsageLogs({
    isAdmin: true,
    searchParams: { username: 'other-user', channel: '99' },
  })

  expect(getUserLogs).not.toHaveBeenCalled()
  expect(getAllLogs).toHaveBeenCalledWith(
    expect.objectContaining({ username: 'other-user', channel: 99 })
  )
  expect(revokeObjectURL).toHaveBeenCalledWith(objectUrl)
})
