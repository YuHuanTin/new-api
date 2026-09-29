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
import { describe, expect, it } from 'vitest'

import { usageLogSchema } from '../../data/schema'

import {
  calculateCommonLogTokenStats,
  mergeCommonLogTokenStats,
} from '../stats'

function makeLog(
  values: Partial<{
    prompt_tokens: number
    completion_tokens: number
    other: string
  }>
) {
  return usageLogSchema.parse({
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: '',
    ...values,
  })
}

describe('calculateCommonLogTokenStats', () => {
  it('aggregates total tokens and cached input across the filtered logs', () => {
    const logs = [
      makeLog({
        prompt_tokens: 100,
        completion_tokens: 50,
        other: JSON.stringify({ image_cache_tokens: 25 }),
      }),
      makeLog({
        prompt_tokens: 300,
        completion_tokens: 150,
        other: JSON.stringify({ cache_tokens: 75, image_cache_tokens: 25 }),
      }),
    ]

    expect(calculateCommonLogTokenStats(logs)).toEqual({
      totalTokens: 600,
      cacheRate: 25,
      inputTokens: 400,
      cachedInputTokens: 100,
    })
  })

  it('merges paged stats using token counts instead of averaging percentages', () => {
    const firstPage = calculateCommonLogTokenStats([
      makeLog({
        prompt_tokens: 100,
        other: JSON.stringify({ cache_tokens: 50 }),
      }),
    ])
    const secondPage = calculateCommonLogTokenStats([
      makeLog({
        prompt_tokens: 900,
        other: JSON.stringify({ cache_tokens: 0 }),
      }),
    ])

    expect(mergeCommonLogTokenStats(firstPage, secondPage)).toMatchObject({
      totalTokens: 1000,
      cacheRate: 5,
    })
  })

  it('returns zeroes when the filtered logs contain no tokens', () => {
    const log = makeLog({ other: '{}' })

    expect(calculateCommonLogTokenStats([log])).toEqual({
      totalTokens: 0,
      cacheRate: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
    })
  })
})
