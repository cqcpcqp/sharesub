import type { Notification, NotificationList, UpdatedCount } from '../types'
import { request } from './client'

export const notificationAPI = {
  notifications: () => request<NotificationList>('/api/notifications'),
  markNotificationRead: (id: string) => request<Notification>(`/api/notifications/${id}`, { method: 'PATCH', body: JSON.stringify({ read: true }) }),
  markAllNotificationsRead: () => request<UpdatedCount>('/api/notifications/read-all', { method: 'POST' }),
}
