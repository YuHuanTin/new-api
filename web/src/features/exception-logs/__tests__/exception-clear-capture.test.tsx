import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type RowSelectionState,
} from '@tanstack/react-table'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { ExceptionClearCapture } from '../components/exception-clear-capture'
import type { ExceptionLog } from '../types'

const logs: ExceptionLog[] = [
  {
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
    other: '{}',
  },
  {
    id: 2,
    created_at: 1788840100,
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
    request_id: 'req-2',
    upstream_request_id: '',
    other: '{}',
  },
]

const columns: ColumnDef<ExceptionLog>[] = [{ id: 'select' }]

function Harness({ selected }: { selected: RowSelectionState }) {
  const [rowSelection, setRowSelection] = useState<RowSelectionState>(selected)
  const table = useReactTable({
    data: logs,
    columns,
    getRowId: (row) => String(row.id),
    state: { rowSelection },
    onRowSelectionChange: setRowSelection,
    enableRowSelection: true,
    getCoreRowModel: getCoreRowModel(),
  })
  return <ExceptionClearCapture table={table} />
}

function renderActions(selected: RowSelectionState) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Harness selected={selected} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

// 没有选中任何请求时不出现清理入口
test('shows no cleanup action without a selection', () => {
  renderActions({})
  expect(screen.queryByRole('button', { name: 'Clear capture' })).toBeNull()
})

// 清理只提交选中的行，成功后清空选择并把胶囊收起来
test('clears the capture of the selected requests only', async () => {
  const user = userEvent.setup()
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: { cleared: 1 } } })

  renderActions({ '1': true })

  await user.click(screen.getByRole('button', { name: 'Clear capture' }))
  expect(screen.getByRole('alertdialog')).toHaveTextContent(
    'Removes the saved request and response payloads of the 1 selected requests'
  )
  await user.click(screen.getByRole('button', { name: 'Clear' }))

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/exception/clear_capture', {
      ids: [1],
    })
  )
  await waitFor(() =>
    expect(screen.queryByRole('button', { name: 'Clear capture' })).toBeNull()
  )
})
