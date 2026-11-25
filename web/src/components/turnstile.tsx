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
import { useEffect, useEffectEvent, useRef } from 'react'

declare global {
  interface Window {
    turnstile?: {
      render: (element: HTMLElement, options: Record<string, unknown>) => string
      remove: (widgetId: string) => void
    }
  }
}

interface TurnstileProps {
  siteKey: string
  onVerify: (token: string) => void
  onExpire?: () => void
  size?: 'normal' | 'compact'
  className?: string
}

export function Turnstile({
  siteKey,
  onVerify,
  onExpire,
  size = 'normal',
  className,
}: TurnstileProps) {
  const ref = useRef<HTMLDivElement | null>(null)
  const verify = useEffectEvent(onVerify)
  const expire = useEffectEvent(() => onExpire?.())

  useEffect(() => {
    const element = ref.current
    if (!element) return

    let active = true
    let widgetId: string | undefined
    const fail = () => {
      if (active) expire()
    }
    const render = () => {
      if (!window.turnstile) {
        fail()
        return
      }
      try {
        widgetId = window.turnstile.render(element, {
          sitekey: siteKey,
          size,
          callback: (token: string) => {
            if (active) verify(token)
          },
          'error-callback': fail,
          'expired-callback': fail,
          'timeout-callback': fail,
          'unsupported-callback': fail,
        })
      } catch {
        fail()
      }
    }

    const dispose = () => {
      active = false
      if (widgetId) window.turnstile?.remove(widgetId)
    }
    if (window.turnstile) {
      render()
      return dispose
    }

    const existing = document.querySelector<HTMLScriptElement>('#cf-turnstile')
    const script = existing ?? document.createElement('script')
    const load = () => {
      window.clearTimeout(timer)
      render()
    }
    const error = () => {
      window.clearTimeout(timer)
      script.remove()
      fail()
    }
    const timer = window.setTimeout(error, 15000)
    script.addEventListener('load', load, { once: true })
    script.addEventListener('error', error, { once: true })
    if (!existing) {
      script.id = 'cf-turnstile'
      script.src =
        'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
      script.async = true
      document.head.appendChild(script)
    }
    return () => {
      dispose()
      window.clearTimeout(timer)
      script.removeEventListener('load', load)
      script.removeEventListener('error', error)
    }
  }, [siteKey, size])

  return <div ref={ref} className={className} />
}
