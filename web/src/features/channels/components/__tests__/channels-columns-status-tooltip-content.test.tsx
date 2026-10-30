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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { channelSchema, type Channel } from '../../types'
import { useChannelsColumns } from '../channels-columns'
import { ChannelsProvider } from '../channels-provider'

function ExampleAutoDisabledStatusCell(props: { channel: Channel }) {
  const table = useReactTable({
    data: [props.channel],
    columns: useChannelsColumns({ enableSelection: false }),
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0]?.getAllCells()
    .find((item) => item.column.id === 'status')

  return cell ? flexRender(cell.column.columnDef.cell, cell.getContext()) : null
}

function renderAutoDisabledChannel(
  channelItem: Channel,
  intervalMinutes: number
) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/option/request_policy') {
      return {
        data: {
          success: true,
          data: {
            options: {
              'monitor_setting.auto_test_channel_minutes':
                String(intervalMinutes),
            },
          },
        },
      }
    }
    return { data: { success: true, data: {} } }
  })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  // 巡检间隔提前塞进缓存，避免单元格在 hover 之后才重渲染
  queryClient.setQueryData(['request-policy'], {
    options: {
      'monitor_setting.auto_test_channel_minutes': String(intervalMinutes),
    },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <ChannelsProvider>
        <ExampleAutoDisabledStatusCell channel={channelItem} />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  return queryClient
}

function autoDisabledChannel(otherInfo: Record<string, unknown>): Channel {
  return channelSchema.parse({
    id: 1,
    type: 1,
    key: 'test-key',
    name: 'Test-Channel-Item',
    status: 3,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    other_info: JSON.stringify(otherInfo),
  })
}

afterEach(() => {
  vi.restoreAllMocks()
})

test('keeps the long string inside the status tooltip is wrapped when show', async () => {
  const reason = '114514'.repeat(40)
  const user = userEvent.setup()

  const client = renderAutoDisabledChannel(
    autoDisabledChannel({ status_reason: reason }),
    0
  )
  await waitFor(() => expect(client.isFetching()).toBe(0))

  await user.hover(screen.getByText('Auto Disabled'))

  expect(await screen.findByText(reason, { exact: false })).toHaveClass(
    'wrap-anywhere'
  )
})

// 自动禁用的渠道要能看出还要等多久才会再被巡检
test('shows the next retry countdown for an auto-disabled channel', async () => {
  const user = userEvent.setup()
  const disabledAt = Math.floor(Date.now() / 1000) - 60

  const client = renderAutoDisabledChannel(
    autoDisabledChannel({
      status_reason: 'upstream failed',
      status_time: disabledAt,
    }),
    10
  )
  await waitFor(() => expect(client.isFetching()).toBe(0))

  await user.hover(screen.getByText('Auto Disabled'))

  // 封禁 1 分钟 + 10 分钟间隔 → 还剩 9 分钟左右（hover 与渲染会耗掉几秒）
  expect(await screen.findByText(/Next retry: in (9m|8m\d+s)/)).toBeVisible()
})
