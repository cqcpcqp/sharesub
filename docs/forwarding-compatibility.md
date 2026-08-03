# Codex 转发兼容说明

ShareSub 复用 sub2api 已验证的 OpenAI Codex 协议行为，但产品边界仍然是“共享 ChatGPT/Codex OAuth 账号”，不是通用多供应商 API 网关。下面的范围用于区分必须保持一致的转发协议与刻意不引入的平台能力。

## 已支持

| 能力 | ShareSub 行为 |
|---|---|
| Responses HTTP 流式请求 | 转发 SSE，逐事件 flush，并记录终止事件 usage |
| Responses HTTP 非流式请求 | 上游使用 SSE，网关在终止事件后返回标准 JSON response |
| Remote compact | 支持三个入口的 `/responses/compact`，转发到 ChatGPT Codex compact 上游 |
| 模型列表 | 普通请求返回支持配置的模型；Codex `client_version` 请求通过所选 OAuth 账号透传实时 manifest 与 ETag |
| 路径兼容 | 支持 `/v1/models`、`/models`、`/backend-api/codex/models`，以及三个 Responses/compact 前缀 |
| OAuth 请求规范化 | 删除 ChatGPT 内部 API 不支持的顶层字段，强制 `store=false`，compact 只保留其协议字段 |
| 会话隔离 | `session_id`、`conversation_id` 和 `prompt_cache_key` 按 ShareSub API Key 隔离 |
| 上游请求头 | 注入 OAuth、ChatGPT Account ID、Codex Beta、Originator、Version 和必要 User-Agent |
| 上游响应头 | 透传请求 ID、Retry-After、Codex 额度窗口和标准 rate-limit 头 |
| 异常流终止 | SSE 在终止事件前断开时补发标准 `response.failed` |
| 安全账号切换 | 只在上游明确返回 `429` 或 `529` 时切换账号，最多切换 3 次 |
| 账号代理 | 支持账号独立代理，且不会在代理失败后静默改为直连 |
| Token 与成本指标 | 记录 Input、Output、Cached Token，并按实际模型和 service tier 计算账号成本 |

## 有意不支持

| sub2api 能力 | 原因 |
|---|---|
| Anthropic、Gemini、Bedrock 等供应商 | 超出 ShareSub 的 Codex 共享边界 |
| `/v1/chat/completions`、`/v1/embeddings` | ChatGPT Codex OAuth 上游不是通用 OpenAI Platform API |
| 独立 Images API | ShareSub 只透传 Responses 内原生工具，不提供 Platform Images API |
| 渠道、分组、余额、充值、倍率计费 | 属于 sub2api 的商业网关模型，不属于共享 Plan |
| 任意 5xx 或连接错误自动重试 | 请求可能已经执行，重试会造成重复工具调用或重复副作用 |

## 尚未支持

Responses WebSocket v2 仍未实现。它需要独立的长连接鉴权、逐帧请求校验、会话粘性、断线恢复、连接级并发占用和 usage 归集，不能通过 HTTP handler 的小范围扩展安全完成。当前生成的 Codex CLI、OpenCode 和 CCS 配置继续使用 Responses HTTP，不依赖 WebSocket v2。
