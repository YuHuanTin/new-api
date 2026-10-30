import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { ExceptionLogsTable } from '../components/exception-logs-table'
import type { ExceptionLog } from '../types'

const log: ExceptionLog = {
  id: 1,
  created_at: 1788840000,
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
  other: JSON.stringify({ request_path: '/v1/chat/completions' }),
}

type LogRequest = Record<string, unknown>

async function renderTable() {
  const requests: LogRequest[] = []
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url === '/api/log/') {
      requests.push((config?.params ?? {}) as LogRequest)
      return {
        data: {
          success: true,
          data: { page: 1, page_size: 20, total: 1, items: [log] },
        },
      }
    }
    return { data: { success: true, data: {} } }
  })

  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const exceptionLogs = createRoute({
    getParentRoute: () => auth,
    path: '/exception-logs/',
    component: ExceptionLogsTable,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([exceptionLogs])]),
    history: createMemoryHistory({ initialEntries: ['/exception-logs/'] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findAllByText('alice')
  await waitFor(() => expect(client.isFetching()).toBe(0))
  return requests
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

test('puts the search button between auto refresh and the view options', async () => {
  await renderTable()

  const autoRefresh = screen.getByRole('button', { name: 'Auto refresh' })
  const search = screen.getByRole('button', { name: 'Search' })
  const viewOptions = screen.getByRole('button', { name: 'View' })

  expect(
    autoRefresh.compareDocumentPosition(search) &
      Node.DOCUMENT_POSITION_FOLLOWING
  ).toBeTruthy()
  expect(
    search.compareDocumentPosition(viewOptions) &
      Node.DOCUMENT_POSITION_FOLLOWING
  ).toBeTruthy()
})

test('refetches the current page when the search button is clicked', async () => {
  const user = userEvent.setup()
  const requests = await renderTable()

  await user.click(screen.getByRole('button', { name: 'Search' }))

  await waitFor(() => expect(requests).toHaveLength(2))
})

// 异常列表默认只看今天：failed_only 的 LIKE 条件必须有时间下界
test('bounds the query with a default time range', async () => {
  const requests = await renderTable()

  expect(typeof requests[0].start_timestamp).toBe('number')
  expect(typeof requests[0].end_timestamp).toBe('number')
  expect(Number(requests[0].end_timestamp)).toBeGreaterThan(
    Number(requests[0].start_timestamp)
  )
})

// 和「通用日志」一样：筛选条件先填，点「搜索」才生效
test('applies the typed filters only after clicking search', async () => {
  const user = userEvent.setup()
  const requests = await renderTable()

  await user.type(screen.getByPlaceholderText('Filter by username...'), 'bob')
  await user.type(
    screen.getByPlaceholderText('Filter by error code...'),
    'cyber_policy'
  )
  expect(requests).toHaveLength(1)

  await user.click(screen.getByRole('button', { name: 'Search' }))

  await waitFor(() => expect(requests).toHaveLength(2))
  expect(requests[1]).toMatchObject({
    username: 'bob',
    error_code: 'cyber_policy',
  })
  expect(typeof requests[1].start_timestamp).toBe('number')
})
