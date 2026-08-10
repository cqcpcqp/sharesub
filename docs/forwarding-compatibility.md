# Codex 转发兼容说明

ShareSub 复用 sub2api 已验证的 OpenAI Codex 协议行为，但产品边界仍然是“共享 ChatGPT/Codex OAuth 账号”，不是通用多供应商 API 网关。下面的范围用于区分必须保持一致的转发协议与刻意不引入的平台能力。

## 已支持

| 能力 | ShareSub 行为 |
|---|---|
| Responses HTTP 流式请求 | 转发 SSE，逐事件 flush，并记录终止事件 usage |
| Responses HTTP 非流式请求 | 上游使用 SSE，网关在终止事件后返回标准 JSON response |
| Remote compact | 支持三个入口的 `/responses/compact`，转发到 ChatGPT Codex compact 上游 |
| 独立联网检索 | 支持三个入口的 `/alpha/search`，按 SearchClient JSON 协议转发到 ChatGPT Codex alpha search 上游 |
| 模型列表 | 普通请求返回支持配置的模型；Codex `client_version` 请求通过所选 OAuth 账号透传实时 manifest 与 ETag |
| Images API | 支持 `/v1/images/generations`、`/v1/images/edits` 及无 `/v1` 别名；OAuth 请求转换为 hosted `image_generation` 工具 |
| 图片输入输出 | 生成支持 JSON；编辑支持 JSON 图片 URL、multipart 图片与 mask；响应支持 JSON 和 Images SSE |
| 图片模型 | `/v1/models` 返回 `gpt-image-1`、`gpt-image-1.5`、`gpt-image-2` |
| 路径兼容 | 支持 `/v1/models`、`/models`、`/backend-api/codex/models`，三个 Responses/compact、Alpha Search 前缀和 Images 前缀 |
| OAuth 请求规范化 | 删除 ChatGPT 内部 API 不支持的顶层字段，强制 `store=false`，compact 只保留其协议字段 |
| 会话隔离 | `session_id`、`conversation_id` 和 `prompt_cache_key` 按 ShareSub API Key 隔离 |
| 上游请求头 | 注入 OAuth、ChatGPT Account ID、Codex Beta、Originator、Version 和必要 User-Agent |
| Alpha Search 请求头 | 使用 SearchClient 独立头部，保留 Codex 身份与 Turn Metadata，不发送 Responses Beta 或会话状态头 |
| 上游响应头 | 透传请求 ID、Retry-After、Codex 额度窗口和标准 rate-limit 头 |
| 异常流终止 | SSE 在终止事件前断开时补发标准 `response.failed` |
| 安全账号切换 | 只在上游明确返回 `429` 或 `529` 时切换账号，最多切换 3 次 |
| 账号代理 | 支持账号独立代理，且不会在代理失败后静默改为直连 |
| Token 与成本指标 | 记录 Input、Output、Cached/Image Token、图片数、独立联网检索次数，并按实际模型、service tier、图片尺寸与 Web Search 次数计算账号成本 |
| Fast/Flex 策略 | 账号规则优先，成员 API Key 规则补充；可按成员、模型和 `service_tier` 透传、过滤、强制 Fast 或拦截，强制 Fast 会为未指定 tier 的请求写入 ChatGPT Codex 兼容值 `priority` |

## 有意不支持

| sub2api 能力 | 原因 |
|---|---|
| Anthropic、Gemini、Bedrock 等供应商 | 超出 ShareSub 的 Codex 共享边界 |
| `/v1/chat/completions`、`/v1/embeddings` | ChatGPT Codex OAuth 上游不是通用 OpenAI Platform API |
| Images 异步任务与对象存储 | ShareSub 没有 Redis/S3 图片任务基础设施；同步 Images API 不依赖该能力 |
| 渠道、分组、余额、充值、倍率计费 | 属于 sub2api 的商业网关模型，不属于共享 Plan |
| 任意 5xx 或连接错误自动重试 | 请求可能已经执行，重试会造成重复工具调用或重复副作用 |

## 尚未支持

Responses WebSocket v2 仍未实现。它需要独立的长连接鉴权、逐帧请求校验、会话粘性、断线恢复、连接级并发占用和 usage 归集，不能通过 HTTP handler 的小范围扩展安全完成。当前生成的 Codex CLI、OpenCode 和 CCS 配置继续使用 Responses HTTP，不依赖 WebSocket v2。
