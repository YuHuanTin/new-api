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
import {
  getCoreRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it } from 'vitest'

import { DataTablePagination } from '../pagination'

const rows = [{ id: 1 }, { id: 2 }, { id: 3 }]
const emptyRows: { id: number }[] = []

function Fixture(props: { empty?: boolean; compact?: boolean }) {
  const table = useReactTable({
    data: props.empty ? emptyRows : rows,
    columns: [{ accessorKey: 'id' }],
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageIndex: 0, pageSize: 2 } },
  })
  return <DataTablePagination table={table} compact={props.compact} />
}
it('moves between pages in compact mode and disables the boundary actions', async () => {
  const user = userEvent.setup()
  render(<Fixture compact />)
  expect(screen.getByText('1 / 2')).toBeVisible()
  expect(
    screen.getByRole('button', { name: 'Go to previous page' })
  ).toBeDisabled()
  await user.click(screen.getByRole('button', { name: 'Go to next page' }))
  expect(screen.getByText('2 / 2')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Go to next page' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: 'Go to previous page' }))
  expect(screen.getByText('1 / 2')).toBeVisible()
})
it('shows a valid empty page with navigation disabled', () => {
  render(<Fixture empty compact />)
  expect(screen.getByText('1 / 1')).toBeVisible()
  expect(
    screen.getByRole('button', { name: 'Go to previous page' })
  ).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Go to next page' })).toBeDisabled()
})
const manyRows = Array.from({ length: 20 }, (_, index) => ({ id: index + 1 }))

function ManyPagesFixture() {
  const table = useReactTable({
    data: manyRows,
    columns: [{ accessorKey: 'id' }],
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageIndex: 0, pageSize: 1 } },
  })
  return <DataTablePagination table={table} />
}

it('virtualizes middle pages and keeps the first and last page fixed', async () => {
  const user = userEvent.setup()
  render(<ManyPagesFixture />)

  expect(screen.getByRole('button', { name: 'Go to page 1' })).toBeVisible()
  expect(screen.getByRole('button', { name: 'Go to page 20' })).toBeVisible()

  const list = document.querySelector<HTMLDivElement>(
    '[data-slot="pagination-pages"]'
  )
  expect(list).not.toBeNull()
  if (!list) throw new Error('pagination list not found')
  // 20 页（每页 1 条）时中间是 2..19，共 18 个页码，但只渲染可视范围附近的一批
  const inner = list.firstElementChild as HTMLElement
  expect(inner.style.width).toBe('680px')
  expect(list.style.width).toBe('148px')
  expect(list.className).toContain('overflow-x-auto')
  expect(list.className).toContain('scrollbar-width:none')
  expect(screen.getByRole('button', { name: 'Go to page 2' })).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Go to page 19' })
  ).not.toBeInTheDocument()
  expect(inner.querySelectorAll('button').length).toBeLessThan(18)

  // 滚轮直接横向滚动
  list.dispatchEvent(
    new WheelEvent('wheel', { deltaY: 120, bubbles: true, cancelable: true })
  )
  expect(list.scrollLeft).toBe(72)

  // 滚到第 10 页附近再点击
  list.scrollLeft = (10 - 2) * 38
  fireEvent.scroll(list)
  await user.click(screen.getByRole('button', { name: 'Go to page 10' }))
  expect(screen.getByRole('button', { name: 'Go to page 10' })).toHaveAttribute(
    'aria-current',
    'page'
  )
})

it('keeps page size selection available in the default layout', () => {
  render(<Fixture />)
  expect(screen.getByRole('combobox')).toBeVisible()
})
