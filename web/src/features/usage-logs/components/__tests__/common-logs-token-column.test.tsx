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
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { usageLogSchema, type UsageLog } from '../../data/schema'
import { useCommonLogsColumns } from '../columns/common-logs-columns'

function TokenColumnView({ log }: { log: UsageLog }) {
  const columns = useCommonLogsColumns(false, false)
  const tokenColumn = columns.find(
    (column) =>
      'accessorKey' in column && column.accessorKey === 'prompt_tokens'
  )
  const cell = tokenColumn?.cell

  if (typeof cell !== 'function') return null
  return cell({ row: { original: log } } as never)
}

describe('common usage log token column', () => {
  it('shows input tokens with cached input removed', () => {
    const log = usageLogSchema.parse({
      id: 1,
      user_id: 1,
      created_at: 1,
      type: 2,
      content: '',
      prompt_tokens: 49137,
      completion_tokens: 348,
      other: JSON.stringify({ cache_tokens: 47872 }),
    })

    render(<TokenColumnView log={log} />)

    expect(screen.getByText('1,265 / 348')).toBeVisible()
    expect(screen.getByText('Cache↓ 47,872')).toBeVisible()
  })

  it('shows Claude input tokens as reported without subtracting cache reads', () => {
    const log = usageLogSchema.parse({
      id: 1,
      user_id: 1,
      created_at: 1,
      type: 2,
      content: '',
      prompt_tokens: 2,
      completion_tokens: 9255,
      other: JSON.stringify({
        claude: true,
        cache_tokens: 46137,
        cache_creation_tokens: 3736,
      }),
    })

    render(<TokenColumnView log={log} />)

    expect(screen.getByText('2 / 9,255')).toBeVisible()
    expect(screen.getByText('Cache↓ 46,137')).toBeVisible()
    expect(screen.getByText('↑ 3,736')).toBeVisible()
  })
})
