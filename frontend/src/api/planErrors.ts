import type { PerformancePeriod, PlanRequestErrorList } from '../types'
import { request } from './client'
import { browserTimezone } from './timezone'

export function requestPlanErrors(path: string, period: PerformancePeriod, page: number, pageSize: number, signal?: AbortSignal, username = '') {
  const timezone = browserTimezone()
  const query = new URLSearchParams({ period, timezone, page: String(page), page_size: String(pageSize), username })
  return request<PlanRequestErrorList>(`${path}?${query}`, { signal })
}
