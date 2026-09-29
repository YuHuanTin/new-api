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
import { getAllLogs, getUserLogs } from '../api'
import type { UsageLog } from '../data/schema'
import { buildApiParams } from './utils'

const EXPORT_PAGE_SIZE = 100

interface ExportUsageLogsConfig {
  isAdmin: boolean
  searchParams: Record<string, unknown>
  columnFilters?: Array<{ id: string; value: unknown }>
}

function downloadUsageLogs(logs: UsageLog[]): void {
  const blob = new Blob([JSON.stringify(logs, null, 2)], {
    type: 'application/json;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `usage-logs-${Date.now()}.json`
  anchor.click()
  URL.revokeObjectURL(url)
}

export async function exportUsageLogs(
  config: ExportUsageLogsConfig
): Promise<void> {
  const logs: UsageLog[] = []
  let page = 1
  let hasMore = true

  while (hasMore) {
    const params = buildApiParams({
      page,
      pageSize: EXPORT_PAGE_SIZE,
      searchParams: config.searchParams,
      columnFilters: config.columnFilters,
      isAdmin: config.isAdmin,
    })
    const response = config.isAdmin
      ? await getAllLogs(params)
      : await getUserLogs(params)

    if (!response.success || !response.data) {
      throw new Error(response.message || 'Failed to export logs')
    }

    const items = response.data.items as UsageLog[]
    logs.push(...items)
    hasMore = items.length > 0 && logs.length < response.data.total
    if (hasMore) page += 1
  }

  downloadUsageLogs(logs)
}
