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
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, expect, test } from 'vitest'

import en from '@/i18n/locales/en.json'

import { TimingMetricsCell } from '../timing-metrics-cell'

const i18n = createInstance()

beforeEach(async () => {
  await i18n.init({
    lng: 'en',
    resources: { en },
    interpolation: { escapeValue: false },
  })
})

function renderCell(maxIdleMs: number | null | undefined) {
  return render(
    <I18nextProvider i18n={i18n}>
      <TimingMetricsCell
        useTimeSec={12}
        completionTokens={100}
        frtMs={800}
        isStream
        maxIdleMs={maxIdleMs}
      />
    </I18nextProvider>
  )
}

test('shows the MIT row with the formatted idle time when max_idle_ms is positive', () => {
  renderCell(2500)

  expect(screen.getByText('MIT')).toBeInTheDocument()
  expect(screen.getByTitle('Maximum idle time')).toHaveTextContent('2.5s')
})

test.each([
  ['absent', undefined],
  ['null', null],
  ['zero', 0],
])('omits the MIT row when max_idle_ms is %s', (_name, maxIdleMs) => {
  renderCell(maxIdleMs)

  expect(screen.queryByText('MIT')).not.toBeInTheDocument()
  expect(screen.queryByTitle('Maximum idle time')).not.toBeInTheDocument()
})
