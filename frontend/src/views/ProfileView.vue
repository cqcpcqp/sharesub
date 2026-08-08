<template>
  <section class="view-content narrow-content">
    <div class="profile-layout">
      <aside class="profile-summary">
        <div class="profile-avatar-wrap">
          <UserAvatar class="profile-avatar" :size="58" :username="user.username" :src="displayedAvatar" />
          <i />
        </div>
        <strong>{{ user.username }}</strong>
        <span>{{ user.email }}</span>
        <StatusBadge :value="user.status" />
        <dl>
          <div><dt>加入时间</dt><dd>{{ formatDate(user.created_at) }}</dd></div>
          <div><dt>用户 ID</dt><dd><code>{{ user.id.slice(0, 10) }}</code></dd></div>
        </dl>
      </aside>

      <div class="settings-section">
        <section class="settings-block">
          <div class="section-heading">
            <div>
              <h2>个人资料</h2>
              <p>用户名会显示在大厅、Plan 成员和性能记录中</p>
            </div>
          </div>
          <form class="settings-form" @submit.prevent="save">
            <div class="avatar-setting">
              <UserAvatar class="profile-editor-avatar" :size="56" :username="user.username" :src="displayedAvatar" />
              <div class="avatar-setting-copy">
                <strong>头像</strong>
                <small>支持 PNG、JPEG 或 WebP，文件不超过 2 MiB。</small>
                <div class="row-actions">
                  <NUpload
                    accept="image/png,image/jpeg,image/webp"
                    :show-file-list="false"
                    :disabled="avatarBusy"
                    :custom-request="uploadAvatar"
                  >
                    <NButton secondary :loading="avatarBusy">
                      <template #icon><Camera :size="16" /></template>
                      {{ user.avatar_url ? '更换头像' : '上传头像' }}
                    </NButton>
                  </NUpload>
                  <NPopconfirm
                    v-if="user.avatar_url"
                    :show-icon="false"
                    positive-text="移除"
                    negative-text="取消"
                    @positive-click="removeAvatar"
                  >
                    <template #trigger>
                      <NButton quaternary type="error" :disabled="avatarBusy">
                        <template #icon><Trash2 :size="16" /></template>
                        移除头像
                      </NButton>
                    </template>
                    确认恢复为用户名首字头像？
                  </NPopconfirm>
                </div>
              </div>
            </div>
            <label>
              用户名
              <AppInput :value="username" clearable :minlength="2" :maxlength="32" :input-props="{ required: true }" @update:value="updateUsername" />
              <small>全局唯一，支持字母、数字、中文、下划线和连字符。</small>
            </label>
            <label>
              邮箱
              <AppInput :value="user.email" disabled />
              <small>邮箱是你的登录凭据，当前不支持修改。</small>
            </label>
            <NButton type="primary" attr-type="submit" :loading="busy" :disabled="!canSaveProfile">
              <template #icon><Save :size="17" /></template>
              保存资料
            </NButton>
          </form>
        </section>

        <section class="settings-block security-settings">
          <div class="section-heading">
            <div>
              <h2>账户安全</h2>
              <p>管理你的登录密码和会话安全</p>
            </div>
          </div>
          <div class="security-preference">
            <div class="preference-heading">
              <span><LockKeyhole :size="18" /></span>
              <div>
                <strong>登录密码</strong>
                <small>修改后，除当前设备外的其他登录会话会立即失效。</small>
              </div>
            </div>
            <NButton secondary @click="passwordDialogOpen = true">修改密码</NButton>
          </div>
        </section>

        <section class="settings-block appearance-settings">
          <div class="section-heading">
            <div>
              <h2>外观</h2>
              <p>选择工作台的显示模式</p>
            </div>
          </div>
          <div class="theme-preference">
            <div class="preference-heading">
              <span><Palette :size="18" /></span>
              <div>
                <strong>界面主题</strong>
                <small>跟随系统会自动匹配设备的浅色或深色外观</small>
              </div>
            </div>
            <NRadioGroup :value="themeMode" class="theme-mode-picker" @update:value="updateThemeMode">
              <NRadioButton v-for="option in themeOptions" :key="option.value" :value="option.value">
                <span class="theme-mode-label"><component :is="option.icon" :size="16" />{{ option.label }}</span>
              </NRadioButton>
            </NRadioGroup>
          </div>
        </section>

        <section class="settings-block release-settings">
          <div class="section-heading">
            <div>
              <h2>关于 ShareSub</h2>
              <p>反馈问题时可以附上版本与构建标识</p>
            </div>
          </div>
          <div class="release-metadata">
            <div class="preference-heading">
              <span><Info :size="18" /></span>
              <div>
                <strong>ShareSub v{{ buildInfo.version }}</strong>
                <small>当前 Web 应用的发布身份</small>
              </div>
            </div>
            <dl>
              <div><dt>版本</dt><dd><code>{{ buildInfo.version }}</code></dd></div>
              <div><dt>构建</dt><dd><code>{{ buildInfo.revision }}</code></dd></div>
            </dl>
          </div>
        </section>
      </div>
    </div>
    <PasswordChangeDialog
      v-if="passwordDialogOpen"
      :forced="false"
      @close="passwordDialogOpen = false"
      @changed="onPasswordChanged"
    />
  </section>
</template>

<script setup lang="ts">
import { NButton, NPopconfirm, NRadioButton, NRadioGroup, NUpload } from 'naive-ui'
import type { UploadCustomRequestOptions } from 'naive-ui'
import { Camera, Info, LockKeyhole, Monitor, Moon, Palette, Save, Sun, Trash2 } from 'lucide-vue-next'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../api'
import AppInput from '../components/AppInput.vue'
import PasswordChangeDialog from '../components/PasswordChangeDialog.vue'
import type { ThemeMode } from '../themePreference'
import type { User } from '../types'
import StatusBadge from '../components/StatusBadge.vue'
import UserAvatar from '../components/UserAvatar.vue'
import { buildInfo } from '../buildInfo'

const props = defineProps<{ user: User; themeMode: ThemeMode }>()
const emit = defineEmits<{
  updated: [user: User]
  message: [type: 'success' | 'error', text: string]
  'update:themeMode': [mode: ThemeMode]
}>()
const username = ref(props.user.username)
const busy = ref(false)
const avatarBusy = ref(false)
const avatarPreview = ref('')
const passwordDialogOpen = ref(false)
const displayedAvatar = computed(() => avatarPreview.value || props.user.avatar_url)
const canSaveProfile = computed(() => {
  const normalized = username.value.trim()
  return normalized.length >= 2 && normalized.length <= 32 && normalized !== props.user.username
})
const themeOptions = [
  { label: '跟随系统', value: 'system' as const, icon: Monitor },
  { label: '浅色', value: 'light' as const, icon: Sun },
  { label: '深色', value: 'dark' as const, icon: Moon },
]

watch(() => props.user.username, value => { username.value = value })
function updateUsername(value: string) { username.value = value }

async function save() {
  if (!canSaveProfile.value || busy.value) return
  busy.value = true
  try {
    const user = await api.updateMe(username.value.trim())
    emit('updated', user)
    emit('message', 'success', '用户名已更新')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    busy.value = false
  }
}

async function uploadAvatar(options: UploadCustomRequestOptions) {
  const file = options.file.file
  if (!file || !['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
    emit('message', 'error', '请选择 PNG、JPEG 或 WebP 图片')
    options.onError()
    return
  }
  if (file.size > 2 * 1024 * 1024) {
    emit('message', 'error', '头像文件不能超过 2 MiB')
    options.onError()
    return
  }

  clearAvatarPreview()
  avatarPreview.value = URL.createObjectURL(file)
  avatarBusy.value = true
  options.onProgress({ percent: 20 })
  try {
    const user = await api.updateAvatar(file)
    options.onProgress({ percent: 100 })
    options.onFinish()
    emit('updated', user)
    emit('message', 'success', '头像已更新')
  } catch (error) {
    options.onError()
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    avatarBusy.value = false
    clearAvatarPreview()
  }
}

async function removeAvatar() {
  avatarBusy.value = true
  try {
    const user = await api.deleteAvatar()
    emit('updated', user)
    emit('message', 'success', '头像已移除')
  } catch (error) {
    emit('message', 'error', error instanceof Error ? error.message : String(error))
  } finally {
    avatarBusy.value = false
  }
}

function clearAvatarPreview() {
  if (!avatarPreview.value) return
  URL.revokeObjectURL(avatarPreview.value)
  avatarPreview.value = ''
}

function updateThemeMode(value: ThemeMode) {
  emit('update:themeMode', value)
}

function onPasswordChanged(user: User) {
  passwordDialogOpen.value = false
  emit('updated', user)
  emit('message', 'success', '密码已更新，其他设备的登录会话已失效')
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'long' }).format(new Date(value))
}

onBeforeUnmount(clearAvatarPreview)
</script>
