import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { CheckinCalendarCard } from '../checkin-calendar-card'

function renderCheckin(checkedToday = false, turnstileEnabled = false) {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        enabled: true,
        stats: {
          checked_in_today: checkedToday,
          total_checkins: 0,
          total_quota: 0,
          checkin_count: 0,
          records: [],
        },
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <CheckinCalendarCard
        checkinEnabled
        turnstileEnabled={turnstileEnabled}
        turnstileSiteKey={turnstileEnabled ? 'test-site-key' : ''}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  document.querySelector('#cf-turnstile')?.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('shows cards immediately and waits for the chosen card to flip before tearing', async () => {
  const matchMedia = window.matchMedia
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
    ...matchMedia(query),
    matches: false,
  }))
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post')
  renderCheckin()

  await user.click(await screen.findByRole('button', { name: 'Check in now' }))

  expect(screen.getAllByRole('button', { name: /^Card \d$/ })).toHaveLength(3)
  expect(post).not.toHaveBeenCalled()

  await user.click(screen.getByRole('button', { name: 'Card 1' }))
  const tear = screen.getByRole('button', { name: 'Tear open card' })
  expect(tear).toBeDisabled()
  const foil = screen.getByRole('img', {
    name: 'Drag across the card to tear it open',
  })
  const flip = foil.closest('.checkin-wish-turn-body')
  if (!flip) throw new Error('Missing card flip animation')
  // jsdom does not run CSS animations.
  fireEvent.animationEnd(flip)
  expect(tear).toBeEnabled()
  expect(tear).toHaveFocus()
  expect(post).not.toHaveBeenCalled()
})

it('keeps the turned card in the center slot with the same sizing as the card backs', async () => {
  const style = document.createElement('style')
  style.textContent = readFileSync(
    resolve(import.meta.dirname, '../../../../styles/checkin-wish.css'),
    'utf8'
  )
  document.head.append(style)

  try {
    const user = userEvent.setup()
    renderCheckin()

    await user.click(
      await screen.findByRole('button', { name: 'Check in now' })
    )
    const centerCard = screen.getByRole('button', { name: 'Card 2' })
    const back = getComputedStyle(centerCard)
    await user.click(centerCard)

    const foil = screen.getByRole('img', {
      name: 'Drag across the card to tear it open',
    })
    const turnedCard = foil.closest('.checkin-wish-turn')
    if (!turnedCard) throw new Error('Missing turned card')
    const front = getComputedStyle(turnedCard)

    expect(front.gridColumn).toBe('2')
    expect(front.aspectRatio).not.toBe('')
    expect(front.aspectRatio).toBe(back.aspectRatio)
    expect(front.minHeight).toBe(back.minHeight)
  } finally {
    style.remove()
  }
})

it('reveals different artwork for each card without submitting a check-in', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post')
  const artwork = new Set<string>()
  renderCheckin()

  for (const card of ['Card 1', 'Card 2', 'Card 3']) {
    await user.click(
      await screen.findByRole('button', { name: 'Check in now' })
    )
    await user.click(screen.getByRole('button', { name: card }))
    const foil = screen.getByRole('img', {
      name: 'Drag across the card to tear it open',
    })
    const source = foil.querySelector('image')?.getAttribute('href')
    expect(source).toBeTruthy()
    if (source) artwork.add(source)
    await user.click(screen.getByRole('button', { name: 'Close' }))
  }

  expect(artwork.size).toBe(3)
  expect(post).not.toHaveBeenCalled()
})

it('reveals the awarded quota and prevents another draw after successful check-in', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { quota_awarded: 500000 } },
  })
  renderCheckin()

  await user.click(await screen.findByRole('button', { name: 'Check in now' }))

  expect(post).not.toHaveBeenCalled()
  expect(screen.getAllByRole('button', { name: /^Card \d$/ })).toHaveLength(3)
  const card = screen.getByRole('button', { name: 'Card 2' })
  card.focus()
  await user.keyboard('{Enter}')
  expect(post).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Tear open card' })).toHaveFocus()
  await user.keyboard('{Enter}')
  expect(await screen.findByRole('status')).toHaveTextContent(
    'Check-in successful! Received'
  )
  expect(post).toHaveBeenCalledExactlyOnceWith('/api/user/checkin')
  expect(
    screen.queryByRole('button', { name: 'Check in now' })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Checked in' })).toBeDisabled()
  expect(
    screen.queryByRole('button', { name: 'Card 2' })
  ).not.toBeInTheDocument()
})

it('allows a new draw when the date changes on an open page', async () => {
  vi.setSystemTime(new Date(2026, 10, 24, 23, 59))
  try {
    const user = userEvent.setup()
    vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: { quota_awarded: 1000 } },
    })
    renderCheckin()

    await user.click(
      await screen.findByRole('button', { name: 'Check in now' })
    )
    await user.click(screen.getByRole('button', { name: 'Card 1' }))
    await user.click(screen.getByRole('button', { name: 'Tear open card' }))
    expect(
      await screen.findByRole('button', { name: 'Checked in' })
    ).toBeDisabled()

    vi.setSystemTime(new Date(2026, 10, 25, 0, 1))
    await user.click(screen.getByRole('button', { name: /Daily Check-in/ }))
    expect(screen.getByRole('button', { name: 'Check in now' })).toBeEnabled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  } finally {
    vi.useRealTimers()
  }
})

it.each(['card edge', 'background', 'fast background swipe'])(
  'submits only after a drag from the %s cuts through the card',
  async (origin) => {
    const user = userEvent.setup()
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: true, data: { quota_awarded: 1000 } },
    })
    renderCheckin()

    await user.click(
      await screen.findByRole('button', { name: 'Check in now' })
    )
    await user.click(screen.getByRole('button', { name: 'Card 1' }))
    const foil = screen.getByRole('img', {
      name: 'Drag across the card to tear it open',
    })
    vi.spyOn(foil, 'getBoundingClientRect').mockReturnValue(
      new DOMRect(0, 0, 100, 145)
    )

    const start =
      origin === 'card edge'
        ? foil
        : screen.getByText('Drag across the card to tear it open')
    fireEvent.pointerDown(start, {
      pointerId: 1,
      button: 0,
      clientX: origin === 'card edge' ? 1 : -100,
      clientY: 14,
    })
    if (origin !== 'fast background swipe') {
      fireEvent.pointerMove(start, { pointerId: 1, clientX: 45, clientY: 10 })
    }
    expect(post).not.toHaveBeenCalled()
    fireEvent.pointerUp(start, { pointerId: 1, clientX: 106, clientY: 12 })

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Check-in successful! Received'
    )
    expect(post).toHaveBeenCalledExactlyOnceWith('/api/user/checkin')
  }
)

it('keeps the draw unavailable when today is already checked in', async () => {
  renderCheckin(true)

  expect(
    await screen.findByRole('button', { name: 'Checked in' })
  ).toBeDisabled()
})

it('allows retry when check-in fails', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'post').mockRejectedValue(new Error('Network error'))
  renderCheckin()

  await user.click(await screen.findByRole('button', { name: 'Check in now' }))
  await user.click(screen.getByRole('button', { name: 'Card 1' }))
  await user.click(screen.getByRole('button', { name: 'Tear open card' }))

  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Card 1' })).toBeEnabled()
  )
  expect(screen.queryByRole('status')).not.toBeInTheDocument()
})

it('starts verification before selection and submits only after both are complete', async () => {
  const user = userEvent.setup()
  let callback: ((token: string) => void) | undefined
  vi.stubGlobal('turnstile', {
    render: vi.fn((_element, options) => {
      callback = options.callback
    }),
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { quota_awarded: 1000 } },
  })
  renderCheckin(false, true)

  await user.click(await screen.findByRole('button', { name: 'Check in now' }))
  await waitFor(() => expect(callback).toBeDefined())
  expect(post).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Card 1' }))
  await user.click(screen.getByRole('button', { name: 'Tear open card' }))
  expect(post).not.toHaveBeenCalled()
  expect(await screen.findByRole('status')).toHaveTextContent(
    'Please complete the security check'
  )
  act(() => callback?.('valid token'))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/checkin?turnstile=valid%20token'
    )
  )
})

it('uses a completed verification only after the chosen card is torn open', async () => {
  const user = userEvent.setup()
  let callback: ((token: string) => void) | undefined
  vi.stubGlobal('turnstile', {
    render: vi.fn((_element, options) => {
      callback = options.callback
    }),
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { quota_awarded: 1000 } },
  })
  renderCheckin(false, true)

  await user.click(await screen.findByRole('button', { name: 'Check in now' }))
  await waitFor(() => expect(callback).toBeDefined())
  act(() => callback?.('ready-token'))
  expect(post).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Card 2' }))
  expect(post).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Tear open card' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledExactlyOnceWith(
      '/api/user/checkin?turnstile=ready-token'
    )
  )
})

it('discards a verification result after the pending draw is closed', async () => {
  const user = userEvent.setup()
  const callbacks: ((token: string) => void)[] = []
  const remove = vi.fn()
  vi.stubGlobal('turnstile', {
    render: vi.fn((_element, options) => {
      callbacks.push(options.callback)
      return String(callbacks.length)
    }),
    remove,
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { quota_awarded: 1000 } },
  })
  renderCheckin(false, true)

  await user.click(await screen.findByRole('button', { name: 'Check in now' }))
  await user.click(screen.getByRole('button', { name: 'Card 1' }))
  await user.click(screen.getByRole('button', { name: 'Tear open card' }))
  expect(await screen.findByRole('status')).toHaveTextContent(
    'Please complete the security check'
  )
  await user.keyboard('{Escape}')
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Check in now' }))
  await user.click(screen.getByRole('button', { name: 'Card 2' }))
  await user.click(screen.getByRole('button', { name: 'Tear open card' }))
  expect(await screen.findByRole('status')).toHaveTextContent(
    'Please complete the security check'
  )
  act(() => callbacks[0]('late-token'))
  expect(post).not.toHaveBeenCalled()
  expect(remove).toHaveBeenCalledWith('1')
  act(() => callbacks[1]('current-token'))
  expect(post).toHaveBeenCalledExactlyOnceWith(
    '/api/user/checkin?turnstile=current-token'
  )
})

it('renders verification after reopening the draw while the script is loading', async () => {
  const user = userEvent.setup()
  vi.stubGlobal('turnstile', undefined)
  renderCheckin(false, true)

  await user.click(await screen.findByRole('button', { name: 'Check in now' }))
  const script = document.querySelector('#cf-turnstile')
  if (!script) throw new Error('Missing Turnstile script')
  await user.keyboard('{Escape}')
  await user.click(screen.getByRole('button', { name: 'Check in now' }))

  const widget = { render: vi.fn(), remove: vi.fn() }
  vi.stubGlobal('turnstile', widget)
  fireEvent.load(script)
  expect(widget.render).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('dialog')).toContainElement(
    widget.render.mock.calls[0][0]
  )
})

it('restarts verification after an expired token without submitting it', async () => {
  const user = userEvent.setup()
  let callbacks:
    | {
        callback: (token: string) => void
        'expired-callback': () => void
      }
    | undefined
  const render = vi.fn((_element, options) => {
    callbacks = options
  })
  vi.stubGlobal('turnstile', { render })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { quota_awarded: 1000 } },
  })
  renderCheckin(false, true)

  await user.click(await screen.findByRole('button', { name: 'Check in now' }))
  await waitFor(() => expect(render).toHaveBeenCalledTimes(1))
  act(() => {
    callbacks?.callback('expired-token')
    callbacks?.['expired-callback']()
  })
  expect(screen.getByRole('alert')).toHaveTextContent('Verification failed')
  await user.click(screen.getByRole('button', { name: 'Card 1' }))
  await user.click(screen.getByRole('button', { name: 'Tear open card' }))
  expect(await screen.findByRole('status')).toHaveTextContent(
    'Please complete the security check'
  )
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => expect(render).toHaveBeenCalledTimes(2))
  expect(post).not.toHaveBeenCalled()
  act(() => callbacks?.callback('fresh-token'))
  expect(post).toHaveBeenCalledExactlyOnceWith(
    '/api/user/checkin?turnstile=fresh-token'
  )
})
