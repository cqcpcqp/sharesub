---
name: ShareSub
description: Access together
colors:
  collaboration-coral: "#df5943"
  collaboration-coral-hover: "#c94734"
  collaboration-coral-pressed: "#b63c2c"
  collaboration-coral-soft: "#fbeae6"
  trust-teal: "#167d70"
  trust-teal-soft: "#e4f2ef"
  routing-blue: "#4b6cdb"
  routing-blue-soft: "#e9eefc"
  warning-amber: "#b47814"
  warning-amber-soft: "#fbf0d9"
  danger-red: "#bf4551"
  danger-red-soft: "#fae9eb"
  performance-purple: "#7656ca"
  performance-purple-soft: "#f0ebfb"
  charcoal-ink: "#222327"
  charcoal-ink-strong: "#151619"
  muted-ink: "#6d7078"
  muted-light: "#92959b"
  divider: "#e2e3df"
  divider-strong: "#d3d5d0"
  warm-canvas: "#f4f5f2"
  paper-surface: "#ffffff"
  soft-surface: "#f8f8f6"
  hover-surface: "#f0f1ee"
  sidebar-surface: "#fbfbf9"
  control-rail: "#e9ebe7"
  info-surface: "#edf6f3"
  info-ink: "#3f5e56"
  info-border: "#c9ddd7"
  danger-ink: "#913740"
  danger-border: "#edc6ca"
  dark-collaboration-coral: "#ed715c"
  dark-collaboration-coral-hover: "#f0816e"
  dark-collaboration-coral-pressed: "#d85d49"
  dark-collaboration-coral-soft: "#3d2422"
  dark-trust-teal: "#35aa99"
  dark-trust-teal-soft: "#173a35"
  dark-routing-blue: "#7895f5"
  dark-routing-blue-soft: "#232f53"
  dark-warning-amber: "#dca64a"
  dark-warning-amber-soft: "#3d3220"
  dark-danger-red: "#e36b76"
  dark-danger-red-soft: "#40252a"
  dark-performance-purple: "#a889ee"
  dark-performance-purple-soft: "#302642"
  dark-ink: "#e7e5e2"
  dark-ink-strong: "#f7f5f2"
  dark-muted: "#a5a19b"
  dark-muted-light: "#858991"
  dark-divider: "#353638"
  dark-divider-strong: "#45474a"
  dark-canvas: "#17181a"
  dark-surface: "#202123"
  dark-surface-soft: "#252628"
  dark-surface-hover: "#2a2b2e"
  dark-sidebar: "#1c1d1f"
  dark-control-rail: "#292a2d"
  dark-info-surface: "#1c2c29"
  dark-info-ink: "#9bcfc7"
  dark-info-border: "#315b54"
  dark-danger-ink: "#ef9aa3"
  dark-danger-border: "#63343b"
typography:
  display:
    fontFamily: '"Avenir Next", "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif'
    fontSize: "clamp(48px, 6vw, 76px)"
    fontWeight: 700
    lineHeight: 1.06
    letterSpacing: "-0.035em"
  headline:
    fontFamily: '"Avenir Next", "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif'
    fontSize: "clamp(28px, 4vw, 42px)"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.025em"
  title:
    fontFamily: '"Avenir Next", "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif'
    fontSize: "24px"
    fontWeight: 760
    lineHeight: 1.2
    letterSpacing: "normal"
  body:
    fontFamily: '"Avenir Next", "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif'
    fontSize: "13px"
    fontWeight: 450
    lineHeight: 1.65
    letterSpacing: "normal"
  label:
    fontFamily: '"Avenir Next", "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif'
    fontSize: "11px"
    fontWeight: 700
    lineHeight: 1.5
    letterSpacing: "normal"
  micro:
    fontFamily: '"Avenir Next", "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif'
    fontSize: "10px"
    fontWeight: 750
    lineHeight: 1.4
    letterSpacing: "0.06em"
  mono:
    fontFamily: '"SFMono-Regular", Consolas, monospace'
    fontSize: "11px"
    fontWeight: 400
    lineHeight: 1.65
    letterSpacing: "normal"
rounded:
  compact: "6px"
  control: "7px"
  surface: "8px"
  surface-raised: "10px"
  marketing: "12px"
  marketing-raised: "14px"
  hero-object: "16px"
  pill: "999px"
spacing:
  xs: "8px"
  sm: "12px"
  md: "16px"
  lg: "20px"
  xl: "24px"
  2xl: "28px"
  3xl: "34px"
components:
  button-primary:
    backgroundColor: "{colors.collaboration-coral}"
    textColor: "{colors.paper-surface}"
    typography: "{typography.label}"
    rounded: "{rounded.control}"
    padding: "0 14px"
    height: "42px"
  button-primary-hover:
    backgroundColor: "{colors.collaboration-coral-hover}"
    textColor: "{colors.paper-surface}"
    rounded: "{rounded.control}"
    height: "42px"
  button-secondary:
    backgroundColor: "{colors.paper-surface}"
    textColor: "{colors.charcoal-ink}"
    typography: "{typography.label}"
    rounded: "{rounded.control}"
    padding: "0 14px"
    height: "42px"
  input:
    backgroundColor: "{colors.soft-surface}"
    textColor: "{colors.charcoal-ink}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "0 12px"
    height: "40px"
  nav-active:
    backgroundColor: "{colors.collaboration-coral-soft}"
    textColor: "{colors.collaboration-coral}"
    typography: "{typography.body}"
    rounded: "{rounded.surface}"
    padding: "0 10px"
    height: "46px"
  status-chip:
    backgroundColor: "{colors.trust-teal-soft}"
    textColor: "{colors.trust-teal}"
    typography: "{typography.micro}"
    rounded: "{rounded.pill}"
    padding: "0 9px"
    height: "25px"
  app-card:
    backgroundColor: "{colors.paper-surface}"
    textColor: "{colors.charcoal-ink}"
    rounded: "{rounded.surface}"
    padding: "20px"
  modal:
    backgroundColor: "{colors.paper-surface}"
    textColor: "{colors.charcoal-ink}"
    rounded: "{rounded.surface}"
    padding: "22px"
---

# Design System: ShareSub

## Overview

**Creative North Star: "清晰协作台 / The Clear Collaboration Desk"**

ShareSub 的视觉系统像一张被认真整理过的共享工作台：每个账号、Plan、成员、额度和状态都有明确位置，信息之间依靠网格、细边框、稳定间距和语义色建立关系。整体气质冷静、精确、克制，但协作珊瑚带来一点温度，避免产品滑向冷硬的基础设施后台。

登录后的产品界面以高效操作为主，保持紧凑密度、短层级和可扫描的数据结构。公开首页是有意的例外：它承担介绍与说服任务，可以使用更大的标题、更宽松的节奏、12–16px 圆角和更强的展示性，但仍共享同一套颜色、字体与克制原则。当前 Logo 是临时资产，不构成未来视觉方向的约束。

**Key Characteristics:**

- 冷静、精确、克制，但带一点温度
- 浅色与深色主题使用同一套语义角色
- 组件精确而克制，操作反馈明确但不过度装饰
- 工作台紧凑高效，公开首页宽松且更具展示感
- 颜色用于表达操作、信任、路由和风险，而非制造噪声

## Colors

浅色主题以暖纸灰画布、白色表面和炭墨文字为基础；深色主题保持相同语义层级，用暖黑表面而不是纯黑。协作珊瑚是品牌和主操作信号，可信青绿、路由蓝、警示琥珀与危险红各自承担稳定的业务语义。

### Primary

- **协作珊瑚** (#df5943)：主按钮、当前导航、关键标题强调与少量品牌面积。悬停使用 #c94734，按下使用 #b63c2c，柔和底色使用 #fbeae6。
- **暗色协作珊瑚** (#ed715c)：暗色主题中的同一语义角色；悬停使用 #f0816e，柔和底色使用 #3d2422。

### Secondary

- **可信青绿** (#167d70)：成功、健康、可用额度与可信说明；柔和底色为 #e4f2ef。
- **路由蓝** (#4b6cdb)：账号、路由、工具性信息和中性数据系列；柔和底色为 #e9eefc。
- **警示琥珀** (#b47814)：提醒、待处理状态和策略配置；柔和底色为 #fbf0d9。
- **危险红** (#bf4551)：失败、停用、撤销和破坏性操作；柔和底色为 #fae9eb。
- **性能紫** (#7656ca)：仪表盘中的性能数据系列；柔和底色为 #f0ebfb。

### Neutral

- **暖纸灰** (#f4f5f2)：浅色主题画布。
- **纸面白** (#ffffff)：卡片、弹窗和前景表面。
- **柔和表面** (#f8f8f6)：输入框、代码区和次级分区。
- **炭墨** (#222327)：正文；**深炭墨** (#151619) 用于标题和关键数值。
- **静音灰** (#6d7078)：说明文字；#92959b 用于更弱的元信息。
- **分隔线** (#e2e3df)：常规边界；#d3d5d0 用于更强边界。
- **暗色画布** (#17181a)、**暗色表面** (#202123) 与 **暗色柔和表面** (#252628)：深色主题的层级基础。
- **暗色正文** (#e7e5e2) 与 **暗色强文字** (#f7f5f2)：深色主题文字。

### Named Rules

**The Signal Scarcity Rule.** 协作珊瑚优先用于主操作、当前位置与少量品牌面积；同一视图中不把所有图标、标签和数据都染成主色。

**The Semantic Accent Rule.** 可信青绿表示成功与健康，路由蓝表示工具与路由，警示琥珀表示提醒，危险红表示失败或破坏性动作；不要为了装饰交换这些含义。

## Typography

**Display Font:** Avenir Next（回退到 Segoe UI、PingFang SC、Microsoft YaHei 和 sans-serif）
**Body Font:** Avenir Next（同一回退栈）
**Label/Mono Font:** SFMono-Regular（回退到 Consolas 和 monospace）

**Character:** 全系统使用一套干净、现代、跨中英文稳定的无衬线字体。差异主要来自尺寸、字重、行高和数字对齐，而不是混用多套字体；产品工作台偏紧凑，公开页面通过更大字号和更宽松行高获得展示感。

### Hierarchy

- **Display**（700，clamp(48px, 6vw, 76px)，1.06）：仅用于公开首页主标题，字距为 -0.035em。
- **Headline**（700，clamp(28px, 4vw, 42px)，1.2）：公开页面分区标题，字距为 -0.025em。
- **Title**（760，24px，1.2）：工作台页面标题和首要数据标题。
- **Body**（450，13px，1.65）：说明文字、公开页正文和需要连续阅读的内容；法律正文可使用 1.85 行高。
- **Label**（700，11px，1.5）：字段标签、指标名称、按钮和表格辅助信息。
- **Micro**（750，10px，1.4，0.06em）：眉题、品牌副标和紧凑元数据；只在短文本中使用大写。
- **Mono**（400，11px，1.65）：API Key、配置、标识符和代码片段，数字指标启用 tabular-nums。

### Named Rules

**The Compact Clarity Rule.** 工作台通过 10–13px 的清晰标签、强弱字重和对齐建立层级，不靠不断放大标题；公开页面才使用 display 与 headline 尺度。

## Layout

登录后的应用采用固定侧边栏与流式工作区：桌面侧边栏宽 264px，可收起到 80px；工作区最大宽度 1510px，常规页面内边距为水平 34px、顶部 28px。卡片网格通常使用 12–18px 间距，表单和控制组以 14–22px 为主要节奏，页面大区块使用 24–34px 分隔。

应用在 1220px 以下减少仪表盘列数，在 940px 以下把侧边栏替换为底部五项导航，在 820px 以下收起登录页插画，在 720px 和 560px 以下逐步把双栏详情、表单、卡片和动作组变为单栏。最小支持宽度为 320px，390px 以下的数据卡片和列表进一步降为单列。移动底栏保留 safe-area-inset-bottom。

公开首页使用最大 1180px 容器，桌面 Hero 为文案与产品示意双栏，主要分区上下留白约 108px。900px 以下切换为单栏，640px 以下使用 16px 页面边距、纵向操作和单列内容。法律页面使用最大 1080px 容器与 250px 目录栏，移动端改为顶部目录。

## Elevation & Depth

系统采用“边框优先、阴影辅助”的混合层次。静态结构主要依靠画布、表面色和 1px 分隔线；卡片只使用轻量环境阴影，悬停时略微抬升。弹窗、通知和公开首页产品示意可以使用更强阴影，因为它们确实位于当前内容之上。

### Shadow Vocabulary

- **Hairline**（0 1px 2px rgba(28, 29, 31, .04)）：状态图标、导航活动图标和最轻卡片。
- **Ambient Card**（0 5px 16px rgba(28, 29, 31, .05)）：搜索框、账号卡片和可交互卡片。
- **Overlay**（0 22px 60px rgba(28, 29, 31, .14)）：通知与应用弹窗。
- **Public Showcase**（0 28px 70px rgba(42, 31, 27, .14)）：仅用于公开首页产品示意。
- **Dark Hairline**（0 1px 2px rgba(0, 0, 0, .18)）、**Dark Ambient**（0 8px 22px rgba(0, 0, 0, .22)）与 **Dark Overlay**（0 24px 70px rgba(0, 0, 0, .44)）：深色主题对应层级。

### Named Rules

**The Border-First Rule.** 默认使用表面色和细边框表达层级；只有交互、浮层或展示对象需要阴影，强阴影不能成为普通卡片的常态。

## Shapes

工作台采用小而稳定的圆角：分段控件内部为 6px，按钮、输入框和紧凑容器为 7px，大多数应用卡片、图标底板和弹窗为 8px。9–10px 仅用于稍强的分组层级，胶囊标签和状态点使用 999px。

公开首页是有意的形状例外：工作流与受众卡片可使用 12px，CTA 使用 14px，产品示意窗口使用 16px。不要把这些营销圆角直接扩散到登录后的数据密集型工作台。

## Components

组件整体遵循“精确而克制”：尺寸稳定、边界清楚、状态完整，反馈通过颜色、边框和轻微位移完成。

### Buttons

- **Shape:** 7px 圆角；小、中、大高度分别为 36px、42px、48px。
- **Primary:** 协作珊瑚底、白色文字、700 字重；中号水平内边距为 14px。
- **Hover / Focus:** 悬停切换到深一阶珊瑚；键盘焦点使用 3px 半透明珊瑚轮廓，不用额外发光动画。
- **Secondary / Ghost:** 白色或透明表面配炭墨文字；图标按钮使用 44px 方形触控区域。

### Chips

- **Style:** 状态标签为胶囊形、10px 高字重文字和 6px 状态点，不显示多余边框。
- **State:** 颜色严格跟随成功、提醒、错误和信息语义；路由标签保持紧凑并允许换行。

### Cards / Containers

- **Corner Style:** 工作台卡片通常为 8px；公开页面卡片为 10–16px。
- **Background:** 前景使用纸面白或暗色表面，次级内容使用柔和表面。
- **Shadow Strategy:** 默认边框加 Hairline 或无阴影，可交互卡片悬停到 Ambient Card 并上移 2px。
- **Border:** 1px 分隔线；空状态和特殊数据表面可以使用虚线边框。
- **Internal Padding:** 紧凑面板通常为 15–20px，弹窗为 22px，公开展示卡片为 24–38px。

### Inputs / Fields

- **Style:** 40px 高、7px 圆角、柔和表面背景、1px 强分隔线，水平内边距 12px。
- **Focus:** 边框切换为协作珊瑚，并使用 0 0 0 3px rgba(223, 89, 67, .12) 焦点环。
- **Error / Disabled:** 错误使用危险红语义族；禁用状态使用 hover-surface 与 muted-light，不改变字段结构。

### Navigation

桌面导航项高 46px、8px 圆角，默认使用静音文字；活动项使用珊瑚柔和底和珊瑚文字，图标置于 30px 区域。940px 以下改为底部五项导航，活动图标取消独立底板以保持移动端简洁。

### Modal

应用弹窗宽度通常为 520px，宽版为 720px，使用 8px 圆角、22px 内边距和 Overlay 阴影。560px 以下弹窗贴底并仅保留顶部 8px 圆角，底部内边距包含 safe area。

### Data Surfaces

指标卡片、Plan 卡片和表格使用 tabular-nums、紧凑标签与语义图标底板。表格表头为 10px 大写标签，行高约 62px；数据卡片不通过装饰图形替代真实数值。

## Do's and Don'ts

### Do:

- **Do** 复用现有 CSS 自定义属性和 Naive UI 主题覆盖，让浅色与深色主题保持同一语义。
- **Do** 在工作台中使用 7–8px 圆角、细边框和紧凑间距，在公开首页保留更宽松的展示尺度。
- **Do** 让协作珊瑚、可信青绿、路由蓝、警示琥珀和危险红维持稳定含义。
- **Do** 用字体层级、对齐和 tabular-nums 提高数据可扫描性。
- **Do** 保留现有 focus-visible 与 prefers-reduced-motion 行为，即使项目没有正式无障碍合规目标。

### Don't:

- **Don't** 把当前 Logo 当作长期视觉权威；它是待重设计的临时资产。
- **Don't** 把公开首页的 12–16px 圆角、大标题和展示阴影扩散到登录后的紧凑工作台。
- **Don't** 在普通卡片上默认使用强阴影、持续浮动或装饰性动效。
- **Don't** 为了“更丰富”而交换语义色、把所有数据都染成主色，或引入通用蓝色 SaaS 视觉。
- **Don't** 把现有轻微径向光晕扩展成大面积渐变、玻璃拟态或与产品无关的科技装饰。
