import type { PlanAllocationMode } from './types'

export function allocationShareBasisPoints(mode: PlanAllocationMode, sharePercent: number): number {
  return mode === 'shared' ? 0 : Math.round(sharePercent) * 100
}

export function allocationModeLabel(mode: PlanAllocationMode): string {
  return mode === 'shared' ? '共享使用' : '固定分配'
}

export function formatShareBasisPoints(value: number): string {
  return `${Math.round(value / 100)}%`
}
