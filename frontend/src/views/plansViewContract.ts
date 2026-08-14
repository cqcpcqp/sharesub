import type { Account, Plan, User } from '../types'
import type { ResolvedTheme } from '../themePreference'

export interface PlansViewComponentProps {
  accounts: Account[]
  plans: Plan[]
  user: User
  theme: ResolvedTheme
  initialPlanId?: string
  invitePlanId?: string
}

export interface PlansViewComponentEmits {
  changed: []
  inviteOpened: []
  message: [type: 'success' | 'error', text: string]
}
