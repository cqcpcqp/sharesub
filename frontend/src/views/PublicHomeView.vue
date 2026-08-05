<template>
  <div class="public-site">
    <header class="public-nav">
      <a class="public-brand" href="/" @click.prevent="emit('navigate', 'home')">
        <BrandMark :size="36" />
        <span><strong>ShareSub</strong><small>Access together</small></span>
      </a>
      <nav aria-label="公开导航">
        <a href="#workflow">工作方式</a>
        <a href="#trust">安全与透明</a>
        <a href="#faq">常见问题</a>
      </nav>
      <div class="public-nav-actions">
        <NButton quaternary @click="emit('login')">登录</NButton>
        <NButton type="primary" @click="emit('register')">免费注册</NButton>
      </div>
    </header>

    <main>
      <section class="public-hero">
        <div class="public-hero-copy">
          <span class="public-eyebrow"><Sparkles :size="14" /> 为 Codex 协作而生</span>
          <h1>一起使用，<br /><em>也各自清楚。</em></h1>
          <p>把你有权使用的 OpenAI Codex 账号接入 ShareSub，通过 Plan 管理成员、分配额度，并让每个人使用独立的 API Key。</p>
          <div class="public-hero-actions">
            <NButton type="primary" size="large" @click="emit('register')">开始使用<template #icon><ArrowRight :size="18" /></template></NButton>
            <NButton size="large" secondary tag="a" href="#workflow">了解工作方式</NButton>
          </div>
          <div class="public-proof">
            <span><ShieldCheck :size="16" /> OAuth 凭据加密保存</span>
            <span><MessageSquareOff :size="16" /> 不保存请求与响应正文</span>
            <span><KeyRound :size="16" /> 每位成员独立密钥</span>
          </div>
        </div>
        <div class="public-hero-visual" aria-label="Plan 配额分配示意">
          <div class="demo-window">
            <header><span><i /><i /><i /></span><small>PLAN OVERVIEW</small></header>
            <div class="demo-plan-heading"><span><Layers3 :size="20" /></span><div><small>共享方案</small><strong>Codex Team Plan</strong></div><b>运行中</b></div>
            <div class="demo-quota"><div><span>5 小时窗口</span><strong>62%</strong></div><i><b /></i><small>成员用量实时归属，额度边界清晰可见</small></div>
            <div class="demo-members">
              <div v-for="member in demoMembers" :key="member.name"><span :style="{ background: member.color }">{{ member.name.slice(0, 1) }}</span><div><strong>{{ member.name }}</strong><small>{{ member.role }}</small></div><b>{{ member.share }}</b></div>
            </div>
            <p>界面数据为产品功能示意，不代表真实用户或公开 Plan。</p>
          </div>
        </div>
      </section>

      <section id="workflow" class="public-section public-workflow">
        <header><span>HOW IT WORKS</span><h2>从账号到协作，只需四步</h2><p>房主掌握账号和共享边界，成员保有各自独立的使用入口。</p></header>
        <div class="workflow-grid">
          <article v-for="(step, index) in workflow" :key="step.title"><b>0{{ index + 1 }}</b><component :is="step.icon" :size="22" /><h3>{{ step.title }}</h3><p>{{ step.description }}</p></article>
        </div>
      </section>

      <section class="public-section public-audiences">
        <article><span><Crown :size="21" /></span><small>对于房主</small><h2>共享访问，不失去控制</h2><ul><li>固定份额或共享使用两种模式</li><li>公开席位、私密邀请和申请审批</li><li>并发、RPM、成员与额度统一管理</li></ul></article>
        <article><span><UsersRound :size="21" /></span><small>对于成员</small><h2>独立使用，消耗清晰可见</h2><ul><li>每个人创建并管理自己的 API Key</li><li>多个 Plan 可配置优先级或均衡选路</li><li>查看个人用量、性能与可用额度</li></ul></article>
      </section>

      <section id="trust" class="public-section public-trust">
        <div><span>SECURITY & TRANSPARENCY</span><h2>可信，不靠一句“请放心”</h2><p>我们只展示当前产品已经实现的安全与透明能力，不用模糊承诺包装产品。</p></div>
        <div class="trust-grid">
          <article v-for="item in trustItems" :key="item.title"><component :is="item.icon" :size="21" /><h3>{{ item.title }}</h3><p>{{ item.description }}</p></article>
        </div>
      </section>

      <section id="faq" class="public-section public-faq">
        <header><span>FAQ</span><h2>开始之前，你可能想知道</h2></header>
        <div><details v-for="item in faq" :key="item.question"><summary>{{ item.question }}<Plus :size="18" /></summary><p>{{ item.answer }}</p></details></div>
      </section>

      <section class="public-cta"><div><span>READY TO START?</span><h2>把共享变成一件清楚的事</h2><p>创建账户，接入你有权使用的账号，并邀请可信成员开始协作。</p></div><NButton type="primary" size="large" @click="emit('register')">免费创建账户<template #icon><ArrowRight :size="18" /></template></NButton></section>
    </main>

    <footer class="public-footer">
      <div class="public-brand"><BrandMark :size="32" /><span><strong>ShareSub</strong><small>Access together</small></span></div>
      <p>ShareSub 是独立产品，与 OpenAI 无隶属、授权或代理关系。OpenAI、ChatGPT 和 Codex 是其各自权利人的商标。</p>
      <nav aria-label="法律文档"><a href="/terms" @click.prevent="emit('navigate', 'terms')">用户协议</a><a href="/privacy" @click.prevent="emit('navigate', 'privacy')">隐私政策</a><a href="/acceptable-use" @click.prevent="emit('navigate', 'acceptable-use')">使用规范</a></nav>
      <small>© {{ new Date().getFullYear() }} ShareSub</small>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { NButton } from 'naive-ui'
import { ArrowRight, Crown, Gauge, KeyRound, Layers3, LockKeyhole, MessageSquareOff, Plus, Route, ShieldCheck, Sparkles, UserRoundPlus, UsersRound } from 'lucide-vue-next'
import BrandMark from '../components/BrandMark.vue'
import type { PublicPageID } from '../appRoutes'

const emit = defineEmits<{ login: []; register: []; navigate: [page: PublicPageID] }>()
const demoMembers = [
  { name: '房主', role: 'Owner · 40%', share: '18.4M', color: '#bf4937' },
  { name: '成员 A', role: 'Member · 30%', share: '11.2M', color: '#247f78' },
  { name: '成员 B', role: 'Member · 30%', share: '8.7M', color: '#5c6fa8' },
]
const workflow = [
  { icon: LockKeyhole, title: '接入账号', description: '房主通过 OpenAI OAuth 接入自己有权使用的 Codex 账号。' },
  { icon: Layers3, title: '创建 Plan', description: '选择固定份额或共享使用，并设定清晰的共享边界。' },
  { icon: UserRoundPlus, title: '邀请成员', description: '私密发送一次性邀请，或公开席位并审批申请。' },
  { icon: KeyRound, title: '独立使用', description: '成员创建自己的 API Key，查看各自用量与性能。' },
]
const trustItems = [
  { icon: LockKeyhole, title: '敏感凭据加密', description: 'OpenAI OAuth 访问凭据使用服务端密钥加密后保存。' },
  { icon: MessageSquareOff, title: '不保存对话正文', description: '网关不把请求或响应正文写入数据库或性能记录。' },
  { icon: Gauge, title: '额度边界明确', description: '同时执行 Codex 5 小时和 7 天窗口的额度管理。' },
  { icon: Route, title: '路由规则可控', description: '按优先级故障转移，或依据可用额度进行均衡选路。' },
]
const faq = [
  { question: 'ShareSub 是 OpenAI 官方产品吗？', answer: '不是。ShareSub 是独立开发的共享管理平台，与 OpenAI 无隶属、授权或代理关系。' },
  { question: '平台会保存我的对话内容吗？', answer: '不会。系统记录额度归属与性能指标，但不把请求或响应正文写入数据库或性能记录。' },
  { question: '任何 OpenAI 账号都可以接入吗？', answer: '你只能接入自己拥有或已获得明确授权使用的账号，并需要遵守 OpenAI 条款及 ShareSub 使用规范。' },
  { question: '公开 Plan 是直接加入吗？', answer: '不是。用户申请公开席位后，仍需由 Plan 房主批准或拒绝。' },
  { question: '成员之间会共用同一个 API Key 吗？', answer: '不会。API Key 归创建它的用户所有，每位成员独立创建、配置和撤销自己的 Key。' },
]
</script>
