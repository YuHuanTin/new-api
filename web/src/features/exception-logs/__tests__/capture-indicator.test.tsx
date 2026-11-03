import { render, renderHook, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, test } from 'vitest'

import en from '@/i18n/locales/en.json'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { useExceptionLogsColumns } from '../components/exception-logs-columns'
import type { ExceptionLog } from '../types'

beforeEach(() => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'root', role: ROLE.SUPER_ADMIN })
})

const i18n = createInstance()

beforeEach(async () => {
  await i18n.init({
    lng: 'en',
    resources: { en },
    interpolation: { escapeValue: false },
  })
})

function makeLog(anomalyCaptured: boolean): ExceptionLog {
  return {
    id: 1,
    created_at: 1788840000,
    type: 5,
    user_id: 1,
    username: 'root',
    token_name: 'demo',
    model_name: 'gpt-3.5-turbo',
    channel_id: 1,
    channel_name: 'Local Mock',
    content: '',
    use_time: 1,
    is_stream: false,
    request_id: 'req-1',
    upstream_request_id: '',
    other: JSON.stringify(
      anomalyCaptured ? { admin_info: { anomaly_captured: true } } : {}
    ),
  }
}

function wrapper(props: { children: ReactNode }) {
  return <I18nextProvider i18n={i18n}>{props.children}</I18nextProvider>
}

function renderDetailsButton(anomalyCaptured: boolean) {
  const { result } = renderHook(() => useExceptionLogsColumns(() => {}), {
    wrapper,
  })
  const actions = result.current.find((column) => column.id === 'actions')
  if (!actions || typeof actions.cell !== 'function') {
    throw new Error('actions column missing')
  }
  const element = actions.cell({
    row: { original: makeLog(anomalyCaptured) },
  } as never)
  return render(<I18nextProvider i18n={i18n}>{element}</I18nextProvider>)
}

test('marks the details button when the row has captured data', () => {
  renderDetailsButton(true)

  const button = screen.getByRole('button', { name: 'Details' })
  expect(button).toHaveClass('bg-primary')
  expect(button).toHaveAttribute('title', 'Captured data available')
})

test('leaves the details button plain when the row has no captured data', () => {
  renderDetailsButton(false)

  const button = screen.getByRole('button', { name: 'Details' })
  expect(button).not.toHaveClass('bg-primary')
  expect(button).not.toHaveAttribute('title')
})

afterEach(() => {
  useAuthStore.setState(useAuthStore.getInitialState(), true)
})

test.each([ROLE.ADMIN, ROLE.USER])(
  'hides capture markers and selection for role %s',
  (role) => {
    useAuthStore.getState().auth.setUser({ id: 2, username: 'viewer', role })
    const { result } = renderHook(() => useExceptionLogsColumns(() => {}), {
      wrapper,
    })
    expect(result.current.some((column) => column.id === 'select')).toBe(false)
    renderDetailsButton(true)
    expect(screen.getByRole('button', { name: 'Details' })).not.toHaveAttribute(
      'title'
    )
  }
)
