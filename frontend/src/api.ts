import { accountAPI } from './api/accounts'
import { adminAPI } from './api/admin'
import { identityAPI } from './api/identity'
import { keyAPI } from './api/keys'
import { notificationAPI } from './api/notifications'
import { planAPI } from './api/plans'

export { APIRequestError, clearSessionToken, sessionToken, setSessionToken } from './api/client'
export { parseOAuthCallback } from './api/accounts'
export type { RegistrationAgreementInput } from './api/identity'
export type { KeyConfigInput } from './api/keys'

// Keep a single stable facade for views and tests while each business area owns
// its request definitions. The fixed backend contract is consumed directly.
export const api = {
  ...identityAPI,
  ...accountAPI,
  ...planAPI,
  ...keyAPI,
  ...notificationAPI,
  ...adminAPI,
}
