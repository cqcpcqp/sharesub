<template>
  <NConfigProvider :theme="naiveTheme" :theme-overrides="activeThemeOverrides">
    <main v-if="authChecking" class="app-bootstrap" aria-label="正在恢复登录状态">
      <BrandMark :size="42" />
      <NSpin size="small" />
    </main>
    <template v-else-if="!user">
      <ThemeSwitcher v-model="themeMode" class="auth-theme-switcher" />
      <AuthView
        :invite-pending="Boolean(inviteIntent)"
        :invitation="invitePreview"
        :invite-loading="invitePreviewLoading"
        :invite-error="inviteError"
        @authenticated="onAuthenticated"
        @retry-invite="loadInvitePreview"
        @discard-invite="discardInvite"
      />
    </template>
    <div v-else-if="user && !user.must_change_password" class="app-shell" :class="{ 'sidebar-collapsed': sidebarCollapsed }">
      <aside class="sidebar">
        <NTooltip placement="right">
          <template #trigger>
            <NButton
              quaternary
              class="icon-button sidebar-toggle"
              :aria-label="sidebarCollapsed ? '展开侧边栏' : '收起侧边栏'"
              :aria-expanded="!sidebarCollapsed"
              @click="sidebarCollapsed = !sidebarCollapsed"
            >
              <template #icon><PanelLeftOpen v-if="sidebarCollapsed" :size="16" /><PanelLeftClose v-else :size="16" /></template>
            </NButton>
          </template>
          {{ sidebarCollapsed ? '展开侧边栏' : '收起侧边栏' }}
        </NTooltip>
        <div class="brand"><BrandMark :size="38" /><div><strong>ShareSub</strong><span>Access together</span></div></div>
        <span class="nav-label">工作台</span>
        <nav aria-label="主导航">
          <NTooltip v-for="item in navItems" :key="item.id" placement="right" :disabled="!sidebarCollapsed">
            <template #trigger>
              <NButton quaternary :class="{ active: activeView === item.id }" :aria-current="activeView === item.id ? 'page' : undefined" @click="navigateToView(item.id)">
                <span class="nav-icon"><component :is="item.icon" :size="18" /></span><span class="nav-text">{{ item.label }}</span><span class="nav-text-mobile">{{ item.shortLabel }}</span>
              </NButton>
            </template>
            {{ item.label }}
          </NTooltip>
        </nav>
        <div class="profile-menu">
          <NTooltip placement="right" :disabled="!sidebarCollapsed">
            <template #trigger>
              <NButton quaternary class="profile-button" :class="{ active: activeView === 'profile' }" @click="navigateToView('profile')">
                <UserAvatar class="user-avatar" :size="36" :username="user.username" :src="user.avatar_url" />
                <span class="profile-copy"><strong>{{ user.username }}</strong><small>{{ user.email }}</small></span>
                <ChevronRight class="profile-chevron" :size="16" />
              </NButton>
            </template>
            {{ user.username }} · 个人设置
          </NTooltip>
          <NTooltip placement="right" :disabled="!sidebarCollapsed">
            <template #trigger>
              <NButton quaternary type="error" class="icon-button sidebar-logout" aria-label="退出登录" @click="logout"><template #icon><LogOut :size="18" /></template></NButton>
            </template>
            退出登录
          </NTooltip>
        </div>
      </aside>

      <main class="workspace">
        <header class="workspace-toolbar">
          <NotificationCenter
            :items="notifications"
            :unread-count="unreadNotificationCount"
            :loading="notificationLoading"
            :reading-all="readingAllNotifications"
            @refresh="refreshNotifications(true)"
            @read="markNotificationRead"
            @read-all="markAllNotificationsRead"
            @open="openNotification"
          />
        </header>
        <div class="workspace-body">
          <Transition name="toast"><NAlert v-if="notice.text" class="notice" :type="notice.type" closable @close="notice.text = ''">{{ notice.text }}</NAlert></Transition>
          <OnboardingGuide v-if="activeView === 'dashboard' && showOnboarding" :accounts="accounts" :plans="plans" :keys="keys" :user="user" @navigate="navigateToView" @invite="openPlanInvite" @setup-key="openKeySetup" />
          <DashboardView v-else-if="activeView === 'dashboard'" :dashboard="dashboard" :loading="busy" :refreshing="dashboardRefreshing" :theme="resolvedTheme" @refresh="refreshDashboard" />
          <LobbyView v-else-if="activeView === 'lobby'" :plans="publicPlans" :user="user" @changed="refreshAll" @message="showMessage" />
          <PlansView v-else-if="activeView === 'plans'" :accounts="accounts" :plans="plans" :user="user" :theme="resolvedTheme" :initial-plan-id="selectedPlanID" :invite-plan-id="invitePlanID" @invite-opened="invitePlanID = ''" @changed="refreshAll" @message="showMessage" />
          <AccountsView v-else-if="activeView === 'accounts'" :accounts="accounts" :plans="plans" @changed="refreshAll" @message="showMessage" />
          <KeysView v-else-if="activeView === 'keys'" :keys="keys" :plans="plans" @changed="refreshAll" @message="showMessage" />
          <AdminView v-else-if="activeView === 'admin' && user.is_admin" :current-user="user" @message="showMessage" />
          <ProfileView v-else v-model:theme-mode="themeMode" :user="user" @updated="onUserUpdated" @message="showMessage" />
        </div>
      </main>
    </div>

    <InvitationStatusDialog
      v-if="user && inviteIntent && (inviteAccepting || inviteError)"
      :accepting="inviteAccepting"
      :error="inviteError"
      :preview="invitePreview"
      @retry="acceptPendingInvite"
      @switch-account="switchInviteAccount"
      @discard="discardInvite"
    />
    <APIKeySetupWizard v-if="user" v-model:show="keySetupVisible" :plans="plans" :initial-plan-id="keySetupPlanID" @created="refreshAll" @message="showMessage" />
    <PasswordChangeDialog v-if="user?.must_change_password" @changed="onPasswordChanged" />
  </NConfigProvider>
</template>

<script setup lang="ts">
import { darkTheme, NAlert, NButton, NConfigProvider, NSpin, NTooltip } from 'naive-ui'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ChevronRight, Compass, KeyRound, Layers3, LayoutDashboard, LogOut, PanelLeftClose, PanelLeftOpen, Settings, ShieldCheck, UsersRound } from 'lucide-vue-next'
import { api, clearSessionToken, sessionToken } from './api'
import type { Account, APIKey, Dashboard, InvitePreview, Notification as UserNotification, Plan, PublicPlan, User } from './types'
import AccountsView from './views/AccountsView.vue'
import AdminView from './views/AdminView.vue'
import APIKeySetupWizard from './components/APIKeySetupWizard.vue'
import AuthView from './views/AuthView.vue'
import BrandMark from './components/BrandMark.vue'
import DashboardView from './views/DashboardView.vue'
import InvitationStatusDialog from './components/InvitationStatusDialog.vue'
import KeysView from './views/KeysView.vue'
import LobbyView from './views/LobbyView.vue'
import NotificationCenter from './components/NotificationCenter.vue'
import OnboardingGuide from './components/OnboardingGuide.vue'
import PasswordChangeDialog from './components/PasswordChangeDialog.vue'
import PlansView from './views/PlansView.vue'
import ProfileView from './views/ProfileView.vue'
import ThemeSwitcher from './components/ThemeSwitcher.vue'
import UserAvatar from './components/UserAvatar.vue'
import { darkThemeOverrides, lightThemeOverrides } from './theme'
import { locationWithoutHash, parseNavigationIntent, type InviteIntent } from './navigationIntent'
import { isThemeMode, resolveTheme, type ThemeMode } from './themePreference'
import { appRoutePath, parseAppRoute, type AppRoute, type ViewID } from './appRoutes'

const nav = [
  { id: 'dashboard' as const, label: '仪表盘', shortLabel: '仪表盘', icon: LayoutDashboard },
  { id: 'lobby' as const, label: '探索大厅', shortLabel: '大厅', icon: Compass },
  { id: 'plans' as const, label: '我的 Plans', shortLabel: 'Plans', icon: Layers3 },
  { id: 'accounts' as const, label: 'OpenAI 账号', shortLabel: '账号', icon: UsersRound },
  { id: 'keys' as const, label: 'API Keys', shortLabel: '密钥', icon: KeyRound },
  { id: 'profile' as const, label: '个人设置', shortLabel: '设置', icon: Settings },
]
const adminNav = { id: 'admin' as const, label: '后台管理', shortLabel: '管理', icon: ShieldCheck }
const user = ref<User | null>(null)
const accounts = ref<Account[]>([])
const plans = ref<Plan[]>([])
const keys = ref<APIKey[]>([])
const publicPlans = ref<PublicPlan[]>([])
const dashboard = ref<Dashboard | null>(null)
const dashboardRefreshing = ref(false)
const initialRoute = parseAppRoute(window.location.pathname)
const activeView = ref<ViewID>(initialRoute?.kind === 'view' ? initialRoute.view : 'dashboard')
const authChecking = ref(true)
const busy = ref(false)
const bootstrapped = ref(false)
const keySetupVisible = ref(false)
const keySetupPlanID = ref('')
const selectedPlanID = ref('')
const invitePlanID = ref('')
const notifications = ref<UserNotification[]>([])
const unreadNotificationCount = ref(0)
const notificationLoading = ref(false)
const readingAllNotifications = ref(false)
const inviteIntent = ref<InviteIntent | null>(parseNavigationIntent(window.location.hash))
const invitePreview = ref<InvitePreview | null>(null)
const invitePreviewLoading = ref(false)
const inviteAccepting = ref(false)
const inviteError = ref('')
const sidebarStorageKey = 'sharesub.sidebar.collapsed'
const sidebarCollapsed = ref(localStorage.getItem(sidebarStorageKey) === 'true')
const themeStorageKey = 'sharesub.theme'
const systemThemeQuery = window.matchMedia('(prefers-color-scheme: dark)')
const storedTheme = localStorage.getItem(themeStorageKey)
const themeMode = ref<ThemeMode>(isThemeMode(storedTheme) ? storedTheme : 'system')
const systemPrefersDark = ref(systemThemeQuery.matches)
const resolvedTheme = computed(() => resolveTheme(themeMode.value, systemPrefersDark.value))
const naiveTheme = computed(() => resolvedTheme.value === 'dark' ? darkTheme : null)
const activeThemeOverrides = computed(() => resolvedTheme.value === 'dark' ? darkThemeOverrides : lightThemeOverrides)
const navItems = computed(() => user.value?.is_admin ? [...nav.slice(0, -1), adminNav, nav[nav.length - 1]] : nav)
const usablePlanIDs = computed(() => new Set(plans.value.map(plan => plan.id)))
const hasUsableKey = computed(() => keys.value.some(key => key.status === 'active' && key.routes.some(route => route.enabled && usablePlanIDs.value.has(route.plan_id))))
const showOnboarding = computed(() => bootstrapped.value && (plans.value.length === 0 || !hasUsableKey.value))
const notice = reactive<{ type: 'success' | 'error'; text: string }>({ type: 'success', text: '' })
let noticeTimer: ReturnType<typeof setTimeout> | undefined
let notificationTimer: ReturnType<typeof setInterval> | undefined
let notificationRequestSequence = 0
watch(themeMode, mode => localStorage.setItem(themeStorageKey, mode), { immediate: true })
watch(resolvedTheme, theme => { document.documentElement.dataset.theme = theme }, { immediate: true })
watch(sidebarCollapsed, collapsed => localStorage.setItem(sidebarStorageKey, String(collapsed)))

function updateSystemTheme(event: MediaQueryListEvent) { systemPrefersDark.value = event.matches }

async function onAuthenticated(value: User) {
  user.value = value
  bootstrapped.value = false
  if (value.must_change_password) {
    navigateToView('dashboard', true)
    return
  }
  startNotificationPolling()
  if (inviteIntent.value) await acceptPendingInvite()
  else {
    navigateToView('dashboard', true)
    await refreshAll()
  }
  await refreshNotifications()
}

async function onPasswordChanged(value: User) {
  user.value = value
  showMessage('success', '密码已更新')
  startNotificationPolling()
  await refreshAll()
  await refreshNotifications()
}

function updateRoute(route: AppRoute, replace = false) {
  const path = appRoutePath(route)
  if (window.location.pathname === path) return
  const location = `${path}${window.location.search}${window.location.hash}`
  if (replace) window.history.replaceState(null, '', location)
  else window.history.pushState(null, '', location)
}

function navigateToView(view: ViewID, replace = false) {
  if (view === 'admin' && !user.value?.is_admin) return
  activeView.value = view
  updateRoute({ kind: 'view', view }, replace)
}

function navigateToLogin(replace = false) { updateRoute({ kind: 'login' }, replace) }

function syncPathRoute() {
  const route = parseAppRoute(window.location.pathname)
  if (user.value) {
    if (route?.kind === 'view' && (route.view !== 'admin' || user.value.is_admin)) activeView.value = route.view
    else navigateToView('dashboard', true)
  } else if (route?.kind !== 'login') navigateToLogin(true)
}

async function refreshAll() {
  if (!user.value) return
  busy.value = true
  try {
    ;[dashboard.value, accounts.value, plans.value, keys.value, publicPlans.value] = await Promise.all([
      api.dashboard(Intl.DateTimeFormat().resolvedOptions().timeZone),
      api.accounts(),
      api.plans(),
      api.keys(),
      api.publicPlans(),
    ])
    bootstrapped.value = true
  } catch (error) {
    showMessage('error', error instanceof Error ? error.message : String(error))
  } finally {
    busy.value = false
  }
}

async function refreshDashboard() {
  if (!user.value || dashboardRefreshing.value || busy.value) return
  const requestUserID = user.value.id
  dashboardRefreshing.value = true
  try {
    const value = await api.dashboard(Intl.DateTimeFormat().resolvedOptions().timeZone)
    if (user.value?.id === requestUserID) dashboard.value = value
  } catch (error) {
    showMessage('error', error instanceof Error ? error.message : String(error))
  } finally {
    dashboardRefreshing.value = false
  }
}

async function refreshNotifications(reportError = false) {
  if (!user.value || notificationLoading.value) return
  const requestUserID = user.value.id
  const requestSequence = ++notificationRequestSequence
  notificationLoading.value = true
  try {
    const result = await api.notifications()
    if (requestSequence !== notificationRequestSequence || user.value?.id !== requestUserID) return
    notifications.value = result.items
    unreadNotificationCount.value = result.unread_count
  } catch (error) {
    if (reportError) showMessage('error', error instanceof Error ? error.message : String(error))
  } finally {
    notificationLoading.value = false
  }
}

async function markNotificationRead(notification: UserNotification) {
  const requestUserID = user.value?.id
  const mutationSequence = ++notificationRequestSequence
  try {
    const updated = await api.markNotificationRead(notification.id)
    if (mutationSequence !== notificationRequestSequence || user.value?.id !== requestUserID) return
    notifications.value = notifications.value.map(item => item.id === updated.id ? updated : item)
    unreadNotificationCount.value = notifications.value.filter(item => !item.read_at).length
  } catch (error) {
    showMessage('error', error instanceof Error ? error.message : String(error))
  }
}

async function markAllNotificationsRead() {
  const requestUserID = user.value?.id
  const mutationSequence = ++notificationRequestSequence
  readingAllNotifications.value = true
  try {
    await api.markAllNotificationsRead()
    if (mutationSequence !== notificationRequestSequence || user.value?.id !== requestUserID) return
    const readAt = new Date().toISOString()
    notifications.value = notifications.value.map(item => item.read_at ? item : { ...item, read_at: readAt })
    unreadNotificationCount.value = 0
  } catch (error) {
    showMessage('error', error instanceof Error ? error.message : String(error))
  } finally {
    readingAllNotifications.value = false
  }
}

async function openNotification(notification: UserNotification) {
  if (notification.resource_type === 'plan') {
    await refreshAll()
    selectedPlanID.value = ''
    await nextTick()
    selectedPlanID.value = notification.resource_id
    navigateToView('plans')
    if (notification.type === 'application_approved') openKeySetup(notification.resource_id)
  } else if (notification.resource_type === 'account') navigateToView('accounts')
  else if (notification.resource_type === 'api_key') navigateToView('keys')
}

function startNotificationPolling() {
  clearInterval(notificationTimer)
  if (document.hidden || !user.value) return
  notificationTimer = setInterval(() => { void refreshNotifications() }, 60_000)
}

function handleVisibilityChange() {
  if (document.hidden) {
    clearInterval(notificationTimer)
    return
  }
  startNotificationPolling()
  void refreshNotifications()
}

async function loadInvitePreview() {
  if (!inviteIntent.value || invitePreviewLoading.value) return
  invitePreviewLoading.value = true
  inviteError.value = ''
  try {
    invitePreview.value = await api.invitePreview(inviteIntent.value.token)
  } catch (error) {
    invitePreview.value = null
    inviteError.value = error instanceof Error ? error.message : String(error)
  } finally {
    invitePreviewLoading.value = false
  }
}

async function acceptPendingInvite() {
  if (!user.value || !inviteIntent.value || inviteAccepting.value) return
  inviteAccepting.value = true
  inviteError.value = ''
  try {
    const member = await api.acceptInvite(inviteIntent.value.token)
    clearInviteIntent()
    await Promise.all([refreshAll(), refreshNotifications()])
    selectedPlanID.value = member.plan_id
    navigateToView('plans')
    openKeySetup(member.plan_id)
    showMessage('success', '已加入 Plan，接下来配置你的 API Key')
  } catch (error) {
    inviteError.value = error instanceof Error ? error.message : String(error)
    await refreshAll()
  } finally {
    inviteAccepting.value = false
  }
}

async function syncNavigationIntent() {
  const nextIntent = parseNavigationIntent(window.location.hash)
  if (nextIntent?.token === inviteIntent.value?.token) return
  inviteIntent.value = nextIntent
  invitePreview.value = null
  inviteError.value = ''
  if (!nextIntent) return
  await loadInvitePreview()
  if (user.value) await acceptPendingInvite()
}

function clearInviteIntent() {
  window.history.replaceState(null, '', locationWithoutHash(window.location.pathname, window.location.search))
  inviteIntent.value = null
  invitePreview.value = null
  inviteError.value = ''
}

function discardInvite() { clearInviteIntent() }
async function switchInviteAccount() { inviteError.value = ''; await logout(); await loadInvitePreview() }
function openPlanInvite(planID: string) { selectedPlanID.value = ''; invitePlanID.value = planID; navigateToView('plans') }
function openKeySetup(planID: string) { keySetupPlanID.value = planID; keySetupVisible.value = true }
function onUserUpdated(value: User) {
  user.value = value
  publicPlans.value = publicPlans.value.map(item => item.plan.owner_user_id === value.id
    ? { ...item, owner_username: value.username, owner_avatar_url: value.avatar_url }
    : item)
}
function showMessage(type: 'success' | 'error', text: string) { notice.type = type; notice.text = text; clearTimeout(noticeTimer); noticeTimer = setTimeout(() => { notice.text = '' }, 5000) }

async function logout() {
  try { await api.logout() } finally {
    notificationRequestSequence += 1
    clearSessionToken()
    clearInterval(notificationTimer)
    user.value = null
    dashboard.value = null
    accounts.value = []
    plans.value = []
    keys.value = []
    publicPlans.value = []
    selectedPlanID.value = ''
    invitePlanID.value = ''
    notifications.value = []
    unreadNotificationCount.value = 0
    bootstrapped.value = false
    activeView.value = 'dashboard'
    navigateToLogin(true)
  }
}

onMounted(async () => {
  systemThemeQuery.addEventListener('change', updateSystemTheme)
  window.addEventListener('hashchange', syncNavigationIntent)
  window.addEventListener('popstate', syncPathRoute)
  document.addEventListener('visibilitychange', handleVisibilityChange)
  if (inviteIntent.value) await loadInvitePreview()
  if (!sessionToken()) {
    syncPathRoute()
    authChecking.value = false
    return
  }
  try {
    user.value = await api.me()
  } catch {
    clearSessionToken()
    user.value = null
    syncPathRoute()
    authChecking.value = false
    return
  }
  syncPathRoute()
  authChecking.value = false
  if (user.value.must_change_password) return
  startNotificationPolling()
  if (inviteIntent.value) await acceptPendingInvite()
  else await refreshAll()
  await refreshNotifications()
})
onBeforeUnmount(() => {
  systemThemeQuery.removeEventListener('change', updateSystemTheme)
  window.removeEventListener('hashchange', syncNavigationIntent)
  window.removeEventListener('popstate', syncPathRoute)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
  clearTimeout(noticeTimer)
  clearInterval(notificationTimer)
})
</script>
