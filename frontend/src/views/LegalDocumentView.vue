<template>
  <div class="legal-site">
    <header class="public-nav legal-nav">
      <a class="public-brand" href="/" @click.prevent="emit('navigate', 'home')"><BrandMark :size="36" /><span><strong>ShareSub</strong><small>Access together</small></span></a>
      <div class="public-nav-actions"><NButton quaternary @click="emit('navigate', 'home')">返回首页</NButton><NButton type="primary" @click="emit('login')">登录</NButton></div>
    </header>
    <main class="legal-layout">
      <aside><span>LEGAL</span><h2>{{ document.title }}</h2><p>版本：{{ document.version }}</p><nav><a v-for="item in documents" :key="item.page" :class="{ active: item.page === page }" :href="item.path" @click.prevent="emit('navigate', item.page)">{{ item.title }}</a></nav></aside>
      <article>
        <header><span>{{ document.englishTitle }}</span><h1>{{ document.title }}</h1><p>生效日期：{{ document.effectiveDate }}</p></header>
        <p class="legal-intro">{{ document.intro }}</p>
        <section v-for="(section, index) in document.sections" :key="section.title"><h2>{{ index + 1 }}. {{ section.title }}</h2><p v-for="paragraph in section.paragraphs" :key="paragraph">{{ paragraph }}</p><ul v-if="section.items"><li v-for="item in section.items" :key="item">{{ item }}</li></ul></section>
      </article>
    </main>
    <footer class="legal-footer"><p>ShareSub 是独立产品，与 OpenAI 无隶属、授权或代理关系。</p><span>© {{ new Date().getFullYear() }} ShareSub</span></footer>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NButton } from 'naive-ui'
import BrandMark from '../components/BrandMark.vue'
import type { PublicPageID } from '../appRoutes'
import { agreementVersions } from '../agreements'

const props = defineProps<{ page: Exclude<PublicPageID, 'home'> }>()
const emit = defineEmits<{ login: []; navigate: [page: PublicPageID] }>()
const documents = [
  { page: 'terms' as const, path: '/terms', title: '用户协议' },
  { page: 'privacy' as const, path: '/privacy', title: '隐私政策' },
  { page: 'acceptable-use' as const, path: '/acceptable-use', title: '可接受使用规范' },
]
interface LegalSection { title: string; paragraphs: readonly string[]; items?: readonly string[] }
interface LegalDocument { title: string; englishTitle: string; version: string; effectiveDate: string; intro: string; sections: readonly LegalSection[] }
const content: Record<Exclude<PublicPageID, 'home'>, LegalDocument> = {
  terms: {
    title: '用户协议', englishTitle: 'TERMS OF SERVICE', version: agreementVersions.terms, effectiveDate: '2026 年 8 月 5 日',
    intro: '欢迎使用 ShareSub。注册、访问或使用本服务前，请认真阅读本协议以及与本协议一并适用的隐私政策和可接受使用规范。',
    sections: [
      { title: '服务说明', paragraphs: ['ShareSub 是面向 OpenAI Codex 使用场景的账号共享管理工具，提供账号接入、Plan 管理、成员协作、额度管理、API Key 管理和请求转发等功能。ShareSub 不是 OpenAI 官方产品，与 OpenAI 无隶属、授权或代理关系。', '平台不销售 OpenAI 账号或上游服务本身。你对上游账号和服务的使用仍受 OpenAI 的条款、政策和限制约束。'] },
      { title: '账户注册与安全', paragraphs: ['你应提供真实、准确且可持续使用的注册信息，并妥善保管密码、会话和 API Key。通过你的账户发起的操作视为由你实施，法律另有规定或你能证明账户被盗用的除外。', '发现账户、会话或密钥可能泄露时，你应及时修改密码、撤销相关密钥并停止异常访问。'] },
      { title: '账号接入与授权', paragraphs: ['你只能接入自己拥有或已获得明确授权使用的 OpenAI 账号。创建 Plan、邀请成员或开放公开招募前，你应确认自己有权授予相应访问。', '房主负责决定成员、额度和共享范围；成员应仅在获准范围内使用。房主与成员之间的授权争议由相关用户自行解决，平台会在必要时采取限制措施以保护系统和其他用户。'] },
      { title: 'Plan 与 API Key', paragraphs: ['Plan 的额度模式在创建后不可更改。固定分配模式按照成员份额管理额度，共享模式由成员共享账号总额度。具体可用性同时受到上游额度、并发、RPM 和服务状态影响。', 'API Key 归创建它的用户所有。你不得泄露、转售或允许无权人员使用密钥，并应在不再使用时及时撤销。'] },
      { title: '服务可用性与变更', paragraphs: ['平台会合理维护服务，但不承诺服务永久不中断、无错误或始终兼容上游变化。OpenAI 接口、模型、额度或政策变化可能影响部分功能。', '为保障安全、合规或改进产品，平台可以调整功能或限制异常访问。涉及本协议的重大变更时，将通过合理方式提示用户。'] },
      { title: '暂停与终止', paragraphs: ['如用户违反本协议、可接受使用规范、适用法律或危害平台安全，平台可以限制相关功能、撤销访问、停用账户或采取必要的保护措施。', '目前平台未提供自助注销入口。如需处理账户或个人信息，请通过平台运营方已公布的渠道提出请求。'] },
      { title: '责任边界', paragraphs: ['你应自行判断共享对象是否可信，并对账号授权和使用行为负责。因上游服务中断、账号限制、用户之间的授权争议或用户未妥善保管凭据造成的损失，按照适用法律确定责任。', '本条不排除依法不能限制或免除的责任。'] },
      { title: '协议适用', paragraphs: ['本协议与隐私政策、可接受使用规范共同构成你与平台之间关于 ShareSub 的约定。如部分条款无效，不影响其余条款的效力。'] },
    ],
  },
  privacy: {
    title: '隐私政策', englishTitle: 'PRIVACY POLICY', version: agreementVersions.privacy, effectiveDate: '2026 年 8 月 17 日',
    intro: '本政策说明 ShareSub 在提供服务过程中处理哪些信息、为什么处理以及如何保存这些信息。我们不会把尚未实现的能力写成承诺。',
    sections: [
      { title: '我们处理的信息', paragraphs: ['为创建和维护账户，我们处理用户名、邮箱、密码哈希、头像、账户状态、登录会话和创建时间。密码不会以明文保存。'], items: ['你主动配置的 OpenAI 账号名称、备注、代理与限速策略', '经加密保存的 OpenAI OAuth 凭据及其有效期', 'Plan、成员、邀请、申请和 API Key 配置', '请求状态、Token 用量、模型、延迟、账号额度状态、成员估算用量和聚合性能指标', '安全与管理操作形成的审计记录和通知'] },
      { title: '请求正文与响应正文', paragraphs: ['ShareSub 网关不把请求正文或响应正文写入数据库或性能记录。请求内容仍会在完成转发所必需的时间内经过服务进程，并被发送给对应的上游服务。'] },
      { title: '使用目的', paragraphs: ['我们处理上述信息，用于身份认证、提供网关和协作功能、执行额度与路由规则、展示用量和性能、排查故障、防止滥用以及维护审计记录。'] },
      { title: '共享与可见范围', paragraphs: ['Plan 成员可以查看该 Plan 所绑定账号的配置、账号额度和成员用量。公开 Plan 会向已登录用户展示 Plan 名称与描述、房主用户名与头像、账号绑定状态、账号订阅类型（如已绑定）、成员数和可用席位。房主应在公开 Plan 前确认这些信息适合展示。', '我们不会因为营销目的出售个人信息。为完成请求，必要数据会被传输至用户所选择的上游服务；上游如何处理数据受其自身政策约束。', '注册和重新发送验证邮件时，我们会把收件邮箱及验证邮件内容传输给腾讯云邮件推送（SES），由其作为事务邮件服务商完成投递。腾讯云对相关数据的处理同时受其隐私与安全规则约束。'] },
      { title: '保存期限', paragraphs: ['会话在到期或退出后失效。系统按照部署配置清理过期资源：网关指标默认保留 90 天，审计记录默认保留 365 天，已读通知及已结束的邀请、申请和撤销 Key 默认保留 90 天；实际部署可以在系统允许范围内调整保留期。未读通知不会自动删除。', '账号和有效业务数据在提供服务所需期间保存，或按照适用法律和有效请求进行处理。'] },
      { title: '安全措施', paragraphs: ['密码使用专用密码哈希算法保存；会话令牌和 API Key 使用哈希值进行鉴权；需要再次展示的完整 API Key 及 OAuth 凭据使用服务端密钥加密保存。', '没有任何系统能够承诺绝对安全。用户也应使用强密码、保护密钥并及时处理异常访问。'] },
      { title: '本地存储', paragraphs: ['前端使用浏览器本地存储保存登录会话令牌、主题偏好和侧边栏偏好，以维持登录状态和界面设置。'] },
      { title: '你的选择与请求', paragraphs: ['你可以在产品内修改用户名、头像和密码，并管理或撤销自己创建的 API Key。目前平台未提供自助注销入口；其他访问、更正或删除请求请通过平台运营方已公布的渠道提出。请求会依据适用法律和账户安全要求处理。'] },
      { title: '政策更新', paragraphs: ['当处理方式发生重大变化时，我们会更新政策版本，并通过合理方式提示用户重新阅读或确认。'] },
    ],
  },
  'acceptable-use': {
    title: '可接受使用规范', englishTitle: 'ACCEPTABLE USE POLICY', version: agreementVersions.acceptableUse, effectiveDate: '2026 年 8 月 5 日',
    intro: '本规范用于保护房主、成员、平台和上游服务。使用 ShareSub 即表示你同意遵守以下边界。',
    sections: [
      { title: '合法且获得授权', paragraphs: ['你只能接入自己拥有或获得明确授权的账号，只能邀请获准成员，并应遵守适用法律、OpenAI 条款及相关政策。'] },
      { title: '禁止的账户与凭据行为', paragraphs: [], items: ['盗用、购买来源不明或未经授权的 OpenAI 账号', '泄露、公开传播、转售或交换他人的 OAuth 凭据、会话或 API Key', '冒充他人、虚构授权关系或诱导他人交付凭据', '在授权终止后继续访问账号或 Plan'] },
      { title: '禁止绕过保护措施', paragraphs: [], items: ['绕过或干扰额度、并发、RPM、身份认证和权限控制', '利用多个账户规避封禁、限制或上游政策', '探测、扫描、攻击平台或其他用户的系统与数据', '发送恶意代码，或实施拒绝服务、自动化滥用和资源耗尽行为'] },
      { title: '禁止违法和有害使用', paragraphs: ['不得利用 ShareSub 生成、传播或协助实施违法、欺诈、侵权、骚扰、恶意攻击或其他违反上游政策的活动。'] },
      { title: '公开 Plan 责任', paragraphs: ['房主不得发布虚假 Plan、伪造席位或以误导信息招募成员。申请人不得通过垃圾申请、骚扰或虚假身份干扰房主。公开信息应准确反映实际可提供的共享范围。'] },
      { title: '执行措施', paragraphs: ['发现违规、安全风险或异常使用时，平台可以限制请求、撤销密钥、下架公开 Plan、暂停账户或保留必要审计信息。措施会结合风险、影响和可用证据确定。'] },
      { title: '报告与申诉', paragraphs: ['如发现账号盗用、凭据泄露或其他违规行为，请通过平台运营方已公布的渠道提交可核验的信息。平台会在现有能力和适用法律范围内处理。'] },
    ],
  },
}
const document = computed(() => content[props.page])
</script>
