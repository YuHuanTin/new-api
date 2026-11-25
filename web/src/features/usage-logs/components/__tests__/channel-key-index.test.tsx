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
import { expect, it } from 'vitest'

import { usageLogSchema, type UsageLog } from '../../data/schema'
import { useCommonLogsColumns } from '../columns/common-logs-columns'
import { UsageLogsProvider } from '../usage-logs-provider'

function ChannelColumnView({ log }: { log: UsageLog }) {
  const columns = useCommonLogsColumns(true, false)
  const cell = columns.find((column) => column.id === 'channel')?.cell
  if (typeof cell !== 'function') return null
  const Cell = cell
  return <Cell {...({ row: { original: log } } as Parameters<typeof Cell>[0])} />
}

it.each([[0, 1], [2, 3]])(
  'displays stored key index %i as key number %i',
  (index, number) => {
    const log = usageLogSchema.parse({
      id: 1,
      user_id: 1,
      created_at: 1,
      type: 2,
      channel: 42,
      content: '',
      other: JSON.stringify({
        admin_info: { is_multi_key: true, multi_key_index: index },
      }),
    })

    render(
      <UsageLogsProvider>
        <ChannelColumnView log={log} />
      </UsageLogsProvider>
    )

    expect(screen.getByLabelText(`Key ${number}`)).toHaveTextContent(
      String(number)
    )
  }
)
