import { request } from './client'
import type { PricingVersion, PricingVersionSummary, PublishPricingInput } from '../pricing'

export const pricingAPI = {
  pricing: () => request<PricingVersion>('/api/pricing'),
  pricingVersion: (id: number) => request<PricingVersion>(`/api/pricing/versions/${id}`),
  pricingHistory: (before = 0) => request<PricingVersionSummary[]>(`/api/pricing/versions?before=${before}`),
  publishPricing: (input: PublishPricingInput) => request<PricingVersion>('/api/admin/pricing/versions', { method: 'POST', body: JSON.stringify(input) }),
}
