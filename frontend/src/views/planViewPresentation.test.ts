import { describe, expect, it } from 'vitest'
import { APIRequestError } from '../api'
import { formatPlanAuditMetadata, planRequestErrorMessage } from './planViewPresentation'

describe('Plan view presentation', () => {
  it('formats fixed audit metadata units', () => {
    expect(formatPlanAuditMetadata('share_basis_points', 2500)).toBe('25%')
    expect(formatPlanAuditMetadata('visibility', 'public')).toBe('公开')
    expect(formatPlanAuditMetadata('allocation_mode', 'fixed')).toBe('固定分配')
  })

  it('maps known API errors without changing unknown messages', () => {
    expect(planRequestErrorMessage(new APIRequestError(409, 'account_already_bound', 'conflict'))).toContain('已绑定其他 Plan')
    expect(planRequestErrorMessage(new APIRequestError(409, 'conflict', 'resource conflict'))).toContain('刷新后重试')
    expect(planRequestErrorMessage(new APIRequestError(400, 'invalid_input', '原始错误'))).toBe('原始错误')
  })
})
