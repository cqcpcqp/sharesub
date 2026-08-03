<template>
  <section class="view-content onboarding-guide">
    <header class="onboarding-intro">
      <span class="onboarding-mark"><Sparkles :size="22" /></span>
      <div><span class="section-kicker">GET STARTED</span><h1>从这里开始</h1><p>完成当前最重要的一步，随后就可以正常共享或使用账号。</p></div>
    </header>

    <div v-if="!path" class="onboarding-paths">
      <article class="onboarding-path">
        <span><Share2 :size="22" /></span>
        <div><h2>发起共享</h2><p>接入你们共同购买的 OpenAI 账号，并建立共享 Plan。</p></div>
        <NButton type="primary" @click="path = 'owner'">开始设置</NButton>
      </article>
      <article class="onboarding-path">
        <span><UserPlus :size="22" /></span>
        <div><h2>加入共享</h2><p>通过朋友发来的邀请链接，或从大厅申请加入 Plan。</p></div>
        <NButton secondary @click="path = 'member'">加入 Plan</NButton>
      </article>
    </div>

    <div v-else class="onboarding-flow">
      <div class="onboarding-progress">
        <div v-for="(step, index) in steps" :key="step.label" class="onboarding-step" :class="{ done: step.done, current: index === currentStep }">
          <span><Check v-if="step.done" :size="15" /><component v-else :is="step.icon" :size="16" /></span>
          <div><strong>{{ step.label }}</strong><small>{{ step.detail }}</small></div>
        </div>
      </div>

      <section class="onboarding-next">
        <div class="onboarding-next-icon"><component :is="nextAction.icon" :size="25" /></div>
        <div><span>下一步</span><h2>{{ nextAction.title }}</h2><p>{{ nextAction.description }}</p></div>
        <div class="onboarding-next-actions">
          <NButton v-if="nextAction.secondaryLabel" secondary @click="runSecondary">{{ nextAction.secondaryLabel }}</NButton>
          <NButton type="primary" @click="runPrimary"><template #icon><component :is="nextAction.icon" :size="17" /></template>{{ nextAction.primaryLabel }}</NButton>
        </div>
      </section>

      <NButton v-if="canSwitchPath" quaternary class="onboarding-switch" @click="path = path === 'owner' ? 'member' : 'owner'">
        {{ path === 'owner' ? '我要加入朋友的 Plan' : '我要共享自己的账号' }}
      </NButton>
    </div>
  </section>
</template>

<script setup lang="ts">
import { NButton } from 'naive-ui'
import { computed, ref, watch } from 'vue'
import { Bot, Check, Compass, KeyRound, Layers3, Share2, Sparkles, UserPlus } from 'lucide-vue-next'
import type { Account, APIKey, Plan, User } from '../types'

type OnboardingPath = 'owner' | 'member'
type ViewID = 'lobby' | 'plans' | 'accounts'

const props = defineProps<{ accounts: Account[]; plans: Plan[]; keys: APIKey[]; user: User }>()
const emit = defineEmits<{ navigate: [view: ViewID]; invite: [planID: string]; setupKey: [planID: string] }>()
const inferredPath = computed<OnboardingPath | null>(() => {
  if (props.plans.some(plan => plan.owner_user_id === props.user.id) || props.accounts.length > 0) return 'owner'
  if (props.plans.length > 0) return 'member'
  return null
})
const selectedPath = ref<OnboardingPath | null>(null)
const path = computed<OnboardingPath | null>({ get: () => inferredPath.value ?? selectedPath.value, set: value => { selectedPath.value = value } })
const ownedPlan = computed(() => props.plans.find(plan => plan.owner_user_id === props.user.id))
const preferredPlan = computed(() => ownedPlan.value ?? props.plans[0])
const inviteStepDone = ref(false)
const usablePlanIDs = computed(() => new Set(props.plans.map(plan => plan.id)))
const hasUsableKey = computed(() => props.keys.some(key => key.status === 'active' && key.routes.some(route => route.enabled && usablePlanIDs.value.has(route.plan_id))))
const canSwitchPath = computed(() => props.accounts.length === 0 && props.plans.length === 0)

watch(ownedPlan, plan => {
  inviteStepDone.value = Boolean(plan && localStorage.getItem(`sharesub.onboarding.invite.${plan.id}`))
}, { immediate: true })

const steps = computed(() => path.value === 'owner'
  ? [
      { label: '接入账号', detail: props.accounts.length ? 'OpenAI 账号已就绪' : '授权共同购买的账号', done: props.accounts.length > 0, icon: Bot },
      { label: '建立 Plan', detail: ownedPlan.value ? ownedPlan.value.name : '创建共享空间', done: Boolean(ownedPlan.value), icon: Layers3 },
      { label: '邀请朋友', detail: inviteStepDone.value ? '邀请入口已就绪' : '生成安全邀请链接', done: inviteStepDone.value, icon: UserPlus },
      { label: '配置密钥', detail: hasUsableKey.value ? 'Codex 已可访问' : '创建自己的访问密钥', done: hasUsableKey.value, icon: KeyRound },
    ]
  : [
      { label: '加入 Plan', detail: props.plans.length ? `已加入 ${props.plans.length} 个 Plan` : '使用邀请链接或探索大厅', done: props.plans.length > 0, icon: UserPlus },
      { label: '配置密钥', detail: hasUsableKey.value ? 'Codex 已可访问' : '创建自己的访问密钥', done: hasUsableKey.value, icon: KeyRound },
    ])
const currentStep = computed(() => Math.max(0, steps.value.findIndex(step => !step.done)))
const nextAction = computed(() => {
  if (path.value === 'owner' && props.accounts.length === 0) return { title: '接入 OpenAI 账号', description: '完成 OpenAI 授权并设置账号的网关策略。', primaryLabel: '接入账号', secondaryLabel: '', icon: Bot, action: 'accounts' as const }
  if (path.value === 'owner' && !ownedPlan.value) return { title: '创建共享 Plan', description: '选择共享使用或固定分配，并建立你们的共享空间。', primaryLabel: '创建 Plan', secondaryLabel: '', icon: Layers3, action: 'plans' as const }
  if (path.value === 'owner' && !inviteStepDone.value) return { title: '邀请你的朋友', description: '生成仅发给成员的邀请链接，对方登录后即可加入。', primaryLabel: '生成邀请链接', secondaryLabel: '稍后邀请', icon: UserPlus, action: 'invite' as const }
  if (path.value === 'member' && props.plans.length === 0) return { title: '加入一个 Plan', description: '邀请链接会在登录后自动接受，也可以从大厅申请公开席位。', primaryLabel: '探索大厅', secondaryLabel: '', icon: Compass, action: 'lobby' as const }
  return { title: '连接你的 Codex', description: '创建独立密钥，并自动绑定当前 Plan。', primaryLabel: '配置 API Key', secondaryLabel: '', icon: KeyRound, action: 'key' as const }
})

function runPrimary() {
  if (nextAction.value.action === 'invite' && ownedPlan.value) {
    emit('invite', ownedPlan.value.id)
  } else if (nextAction.value.action === 'key' && preferredPlan.value) emit('setupKey', preferredPlan.value.id)
  else if (nextAction.value.action === 'accounts' || nextAction.value.action === 'plans' || nextAction.value.action === 'lobby') emit('navigate', nextAction.value.action)
}

function runSecondary() {
  if (!ownedPlan.value) return
  completeInviteStep(ownedPlan.value.id)
}

function completeInviteStep(planID: string) {
  localStorage.setItem(`sharesub.onboarding.invite.${planID}`, 'done')
  inviteStepDone.value = true
}
</script>
