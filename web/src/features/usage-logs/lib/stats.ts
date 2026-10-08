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
import type { UsageLog } from '../data/schema'
import { parseLogOther } from './format'

export interface CommonLogTokenStats {
  totalTokens: number
  cacheRate: number
  inputTokens: number
  cachedInputTokens: number
}

function createCommonLogTokenStats(
  totalTokens: number,
  inputTokens: number,
  cachedInputTokens: number
): CommonLogTokenStats {
  const cacheRate =
    inputTokens > 0
      ? Math.min(100, (cachedInputTokens / inputTokens) * 100)
      : 0

  return { totalTokens, cacheRate, inputTokens, cachedInputTokens }
}

function nonNegative(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
    ? value
    : 0
}

/**
 * Derive token totals from the logs already selected by the active filters.
 * Cache writes are excluded from the hit rate because they are not cache hits.
 * Image cache tokens are a subset of cache_tokens when both are reported.
 */
export function calculateCommonLogTokenStats(
  logs: UsageLog[]
): CommonLogTokenStats {
  let totalInputTokens = 0
  let totalTokens = 0
  let cachedInputTokens = 0

  for (const log of logs) {
    const promptTokens = nonNegative(log.prompt_tokens)
    const completionTokens = nonNegative(log.completion_tokens)
    const other = parseLogOther(log.other)
    const cachedTokens = Math.max(
      nonNegative(other?.cache_tokens),
      nonNegative(other?.image_cache_tokens)
    )
    // Claude's prompt_tokens excludes cache reads and writes, so add them back
    // to get the total input that the cache rate is measured against.
    const cacheWriteTokens =
      nonNegative(other?.cache_creation_tokens_5m) +
        nonNegative(other?.cache_creation_tokens_1h) ||
      nonNegative(other?.cache_creation_tokens)
    const inputTokens =
      other?.claude === true
        ? promptTokens + cachedTokens + cacheWriteTokens
        : promptTokens

    totalInputTokens += inputTokens
    totalTokens += inputTokens + completionTokens
    cachedInputTokens += cachedTokens
  }

  return createCommonLogTokenStats(
    totalTokens,
    totalInputTokens,
    cachedInputTokens
  )
}

export function mergeCommonLogTokenStats(
  ...stats: CommonLogTokenStats[]
): CommonLogTokenStats {
  return createCommonLogTokenStats(
    stats.reduce((sum, item) => sum + item.totalTokens, 0),
    stats.reduce((sum, item) => sum + item.inputTokens, 0),
    stats.reduce((sum, item) => sum + item.cachedInputTokens, 0)
  )
}
