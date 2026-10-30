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
import type { Table } from '@tanstack/react-table'
import {
  ChevronLeft as ChevronLeftIcon,
  ChevronRight as ChevronRightIcon,
  ChevronsLeft as DoubleArrowLeftIcon,
  ChevronsRight as DoubleArrowRightIcon,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'

type DataTablePaginationProps<TData> = {
  table: Table<TData>
  compact?: boolean
}

const PAGE_SIZE_OPTIONS = [10, 20, 30, 40, 50, 100] as const
const PAGE_SIZE_SELECT_ITEMS = PAGE_SIZE_OPTIONS.map((pageSize) => ({
  value: `${pageSize}`,
  label: pageSize,
}))
const PAGE_GAP = 4
const VIEWPORT_PAGE_COUNT = 4
const OVERSCAN_PAGES = 3
// 滚轮滚动距离打个折，一格滚轮不会一下跳过太多页码
const WHEEL_SCROLL_FACTOR = 0.6

export function DataTablePagination<TData>({
  table,
  compact = false,
}: DataTablePaginationProps<TData>) {
  const { t } = useTranslation()
  const pagination = table.getState().pagination
  const currentPage = pagination.pageIndex + 1
  const pageSize = pagination.pageSize
  const totalPages = table.getPageCount()
  const totalRows = table.getRowCount()
  const middlePageCount = Math.max(0, totalPages - 2)
  // 中间页码按同一宽度排版，虚拟滚动才能用固定步长算出可视范围
  const pageDigits = String(Math.max(totalPages, 1)).length
  const pageWidth = Math.max(32, pageDigits * 9 + 16)
  const pageStep = pageWidth + PAGE_GAP
  const trackWidth =
    middlePageCount > 0
      ? middlePageCount * pageWidth + (middlePageCount - 1) * PAGE_GAP
      : 0
  const maxViewportWidth =
    VIEWPORT_PAGE_COUNT * pageWidth + (VIEWPORT_PAGE_COUNT - 1) * PAGE_GAP
  const viewportWidth =
    middlePageCount > 0 ? Math.min(maxViewportWidth, trackWidth) : 0

  const [scrollLeft, setScrollLeft] = useState(0)
  const pageListRef = useRef<HTMLDivElement>(null)

  // 虚拟列表：只渲染可视范围附近的页码，滚到哪就换哪一批
  const firstVisible = Math.floor(scrollLeft / pageStep)
  const lastVisible = Math.floor((scrollLeft + viewportWidth - 1) / pageStep)
  const visibleStart = Math.max(0, firstVisible - OVERSCAN_PAGES)
  const visibleEnd = Math.min(middlePageCount - 1, lastVisible + OVERSCAN_PAGES)
  const visibleMiddlePages: { index: number; page: number }[] = []
  for (let index = visibleStart; index <= visibleEnd; index++) {
    visibleMiddlePages.push({ index, page: index + 2 })
  }

  // 当前页变化时把滚动位置对准它，首尾页则贴住两端
  useEffect(() => {
    const list = pageListRef.current
    if (!list || middlePageCount === 0) return
    const maxScroll = Math.max(0, trackWidth - viewportWidth)
    let target = 0
    if (currentPage >= totalPages - 1) {
      target = maxScroll
    } else if (currentPage > 2) {
      target = (currentPage - 2) * pageStep - (viewportWidth - pageWidth) / 2
    }
    target = Math.max(0, Math.min(target, maxScroll))
    list.scrollLeft = target
    setScrollLeft(target)
  }, [
    currentPage,
    totalPages,
    middlePageCount,
    pageStep,
    viewportWidth,
    trackWidth,
    pageWidth,
  ])

  // 垂直滚轮直接横向翻页
  useEffect(() => {
    const list = pageListRef.current
    if (!list) return
    const handleWheel = (event: WheelEvent) => {
      if (event.deltaY === 0) return
      event.preventDefault()
      list.scrollLeft += event.deltaY * WHEEL_SCROLL_FACTOR
      setScrollLeft(list.scrollLeft)
    }
    list.addEventListener('wheel', handleWheel, { passive: false })
    return () => list.removeEventListener('wheel', handleWheel)
  }, [middlePageCount])

  const renderPage = (page: number) => (
    <Button
      key={page}
      variant={currentPage === page ? 'default' : 'outline'}
      aria-label={t('Go to page {{page}}', { page })}
      aria-current={currentPage === page ? 'page' : undefined}
      style={{ width: pageWidth }}
      className={cn(
        'h-8 min-w-8 shrink-0 px-2 tabular-nums',
        currentPage === page
          ? 'font-semibold'
          : 'text-muted-foreground hover:text-foreground'
      )}
      onClick={() => table.setPageIndex(page - 1)}
    >
      {page}
    </Button>
  )

  if (compact) {
    return (
      <nav
        aria-label={t('Page')}
        className='flex w-full min-w-0 flex-wrap items-center justify-between gap-2 text-sm'
      >
        <span className='text-muted-foreground min-w-0 [overflow-wrap:anywhere]'>
          {t('Total:')} {totalRows.toLocaleString()}
        </span>
        <div className='flex items-center gap-2'>
          <Button
            variant='outline'
            size='icon'
            className='size-11'
            aria-label={t('Go to previous page')}
            onClick={() => table.previousPage()}
            disabled={!table.getCanPreviousPage()}
          >
            <ChevronLeftIcon />
          </Button>
          <span className='tabular-nums' aria-live='polite'>
            {currentPage} / {Math.max(1, totalPages)}
          </span>
          <Button
            variant='outline'
            size='icon'
            className='size-11'
            aria-label={t('Go to next page')}
            onClick={() => table.nextPage()}
            disabled={!table.getCanNextPage()}
          >
            <ChevronRightIcon />
          </Button>
        </div>
      </nav>
    )
  }

  return (
    <div
      className={cn(
        '@container/pagination flex min-w-0 items-center justify-end overflow-clip'
      )}
      style={{ overflowClipMargin: 1 }}
    >
      <div className='flex min-w-0 items-center gap-2 @xl/pagination:gap-3'>
        <div className='flex shrink-0 items-baseline gap-1.5 text-xs font-medium whitespace-nowrap sm:text-sm'>
          <span className='text-muted-foreground/80'>{t('Total:')}</span>
          <span className='text-foreground tabular-nums'>
            {totalRows.toLocaleString()}
          </span>
        </div>

        <div className='flex shrink-0 items-center gap-1.5 @lg/pagination:gap-2'>
          <p className='text-muted-foreground/80 hidden text-sm font-medium whitespace-nowrap @2xl/pagination:block'>
            {t('Rows per page')}
          </p>
          <Select
            items={PAGE_SIZE_SELECT_ITEMS}
            value={`${pageSize}`}
            onValueChange={(value) => {
              table.setPageSize(Number(value))
            }}
          >
            <SelectTrigger className='text-foreground h-8 w-[64px] font-medium tabular-nums sm:w-[70px]'>
              <SelectValue placeholder={pageSize} />
            </SelectTrigger>
            <SelectContent side='top' alignItemWithTrigger={false}>
              <SelectGroup>
                {PAGE_SIZE_OPTIONS.map((pageSize) => (
                  <SelectItem key={pageSize} value={`${pageSize}`}>
                    {pageSize}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>

        <div className='flex min-w-0 items-center gap-1 @lg/pagination:gap-1.5 @xl/pagination:gap-2'>
          <Button
            variant='outline'
            className='text-muted-foreground hover:text-foreground disabled:text-muted-foreground/50 size-8 shrink-0 p-0 @max-lg/pagination:hidden'
            onClick={() => table.setPageIndex(0)}
            disabled={!table.getCanPreviousPage()}
          >
            <span className='sr-only'>{t('Go to first page')}</span>
            <DoubleArrowLeftIcon className='h-4 w-4' />
          </Button>
          <Button
            variant='outline'
            className='text-muted-foreground hover:text-foreground disabled:text-muted-foreground/50 size-8 shrink-0 p-0'
            onClick={() => table.previousPage()}
            disabled={!table.getCanPreviousPage()}
          >
            <span className='sr-only'>{t('Go to previous page')}</span>
            <ChevronLeftIcon className='h-4 w-4' />
          </Button>

          {totalPages > 0 && renderPage(1)}
          {middlePageCount > 0 && (
            <div
              ref={pageListRef}
              data-slot='pagination-pages'
              onScroll={(event) =>
                setScrollLeft(event.currentTarget.scrollLeft)
              }
              style={{ width: viewportWidth }}
              className='relative min-w-0 shrink [scrollbar-width:none] overflow-x-auto overscroll-x-contain [-ms-overflow-style:none] [&::-webkit-scrollbar]:hidden'
            >
              <div className='relative h-8' style={{ width: trackWidth }}>
                {visibleMiddlePages.map(({ index, page }) => (
                  <div
                    key={page}
                    className='absolute top-0'
                    style={{ left: index * pageStep }}
                  >
                    {renderPage(page)}
                  </div>
                ))}
              </div>
            </div>
          )}
          {totalPages > 1 && renderPage(totalPages)}

          <Button
            variant='outline'
            className='text-muted-foreground hover:text-foreground disabled:text-muted-foreground/50 size-8 shrink-0 p-0'
            onClick={() => table.nextPage()}
            disabled={!table.getCanNextPage()}
          >
            <span className='sr-only'>{t('Go to next page')}</span>
            <ChevronRightIcon className='h-4 w-4' />
          </Button>
          <Button
            variant='outline'
            className='text-muted-foreground hover:text-foreground disabled:text-muted-foreground/50 size-8 shrink-0 p-0 @max-lg/pagination:hidden'
            onClick={() => table.setPageIndex(table.getPageCount() - 1)}
            disabled={!table.getCanNextPage()}
          >
            <span className='sr-only'>{t('Go to last page')}</span>
            <DoubleArrowRightIcon className='h-4 w-4' />
          </Button>
        </div>
      </div>
    </div>
  )
}
