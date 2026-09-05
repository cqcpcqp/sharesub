# Codex 转发兼容说明

ShareSub 复用 sub2api 已验证的 OpenAI Codex 协议行为，但产品边界仍然是“共享 ChatGPT/Codex OAuth 账号”，不是通用多供应商 API 网关。下面的范围用于区分必须保持一致的转发协议与刻意不引入的平台能力。

## 已支持

| 能力 | ShareSub 行为 |
|---|---|
| Responses HTTP 流式请求 | 转发 SSE，逐事件 flush，并记录终止事件 usage |
| Responses HTTP 非流式请求 | 上游使用 SSE，网关在终止事件后返回标准 JSON response |
| Responses WebSocket v2 | 三个 Responses 入口都支持 `GET` + WebSocket Upgrade；客户端与 ChatGPT Codex 上游保持端到端 WebSocket，一条连接内可串行执行多个 `response.create` turn |
| WebSocket 会话与并发 | 连接绑定首次选定的账号并跨 turn 复用上游连接；账号并发和 RPM 按 turn 获取，终止事件后立即释放，轮间空闲不占账号业务并发 |
| WebSocket 资源边界 | 首包默认等待 30 秒，轮间默认空闲 5 分钟，会话最长 1 小时；单实例中每个用户 API Key 最多 64 条连接，客户端与上游单条消息默认限制为 64 MiB 和 16 MiB |
| WebSocket 逐 turn 指标 | 每个终止 turn 独立记录模型、TTFT、总耗时和固定 usage；连接断开与协议错误不会把整条连接错误地合并成一次请求 |
| Remote compact | 支持三个入口的 `/responses/compact`，转发到 ChatGPT Codex compact 上游 |
| 独立联网检索 | 支持三个入口的 `/alpha/search`，按 SearchClient JSON 协议转发到 ChatGPT Codex alpha search 上游 |
| 模型列表 | 普通请求返回支持配置的模型；Codex `client_version` 请求通过所选 OAuth 账号透传实时 manifest 与 ETag |
| GPT-6 Astra | 静态模型列表提供官方 ID `gpt-6-astra`；Responses 请求按官方兼容规则移除不支持的采样/logprobs 参数，并将 `none`、`minimal` reasoning effort 规范为 `low` |
| Images API | 支持 `/v1/images/generations`、`/v1/images/edits` 及无 `/v1` 别名；OAuth 请求转换为 hosted `image_generation` 工具 |
| 图片输入输出 | 生成支持 JSON；编辑支持 JSON 图片 URL、multipart 图片与 mask；响应支持 JSON 和 Images SSE |
| 图片模型 | `/v1/models` 返回 `gpt-image-1`、`gpt-image-1.5`、`gpt-image-2` |
| 路径兼容 | 支持 `/v1/models`、`/models`、`/backend-api/codex/models`，三个 Responses/compact、Alpha Search 前缀和 Images 前缀；三个非 compact Responses 路径同时接受 WebSocket Upgrade |
| OAuth 请求规范化 | 删除 ChatGPT 内部 API 不支持的顶层字段与采样参数，规范字符串/对象 `input` 为数组，强制 `store=false`；携带 reasoning 时请求加密推理上下文，compact 只保留其协议字段 |
| 会话隔离 | `session_id`、`conversation_id` 和 `prompt_cache_key` 按 ShareSub API Key 隔离 |
| 上游请求头 | 注入 OAuth、ChatGPT Account ID、Originator、Version 和必要 User-Agent；移除废弃的 `responses=experimental` Beta，同时保留客户端发送的其他独立 Beta token |
| Alpha Search 请求头 | 使用 SearchClient 独立头部，保留 Codex 身份与 Turn Metadata，不发送 Responses Beta 或会话状态头 |
| 上游响应头 | 透传请求 ID、Retry-After、Codex 额度窗口和标准 rate-limit 头 |
| 异常流终止 | SSE 在终止事件前断开时补发标准 `response.failed` |
| 安全账号切换 | 只在上游明确返回 `429` 或 `529` 时切换账号，最多切换 3 次 |
| 账号代理 | 支持账号独立代理，且不会在代理失败后静默改为直连 |
| Token 与成本指标 | 记录 Input、Output、Cached/Image Token、图片数、独立联网检索次数，并按实际模型、service tier、图片尺寸与 Web Search 次数计算账号成本 |
| GPT-6 计费 | 按官方 Standard、Fast/Priority、Flex 和缓存读写价格计费；输入超过 272K 时按官方长上下文倍率计算整次请求 |
| Fast/Flex 策略 | 账号规则优先，成员 API Key 规则补充；可按成员、模型和 `service_tier` 透传、过滤、强制 Fast 或拦截，强制 Fast 会为未指定 tier 的请求写入 ChatGPT Codex 兼容值 `priority` |

## 有意不支持

| sub2api 能力 | 原因 |
|---|---|
| Anthropic、Gemini、Bedrock 等供应商 | 超出 ShareSub 的 Codex 共享边界 |
| `/v1/chat/completions`、`/v1/embeddings` | ChatGPT Codex OAuth 上游不是通用 OpenAI Platform API |
| Images 异步任务与对象存储 | ShareSub 没有 Redis/S3 图片任务基础设施；同步 Images API 不依赖该能力 |
| 渠道、分组、余额、充值、倍率计费 | 属于 sub2api 的商业网关模型，不属于共享 Plan |
| 任意 5xx 或连接错误自动重试 | 请求可能已经执行，重试会造成重复工具调用或重复副作用 |

## Responses WebSocket v2 边界

- 同一连接一次只允许一个正在执行的 response；收到终止事件后才接受下一轮 `response.create`。
- 活动响应支持透传 `response.steer`；原响应无论以 `response.incomplete(reason=steered)` 还是正常 `response.completed` 结束，都会在当前逻辑 turn 等待自动 continuation；各段 usage 汇总展示、逐响应计费。需要工具结果时支持 `response.steer.pending` 及客户端提前提交匹配的 `response.create`。
- 会话建立后固定使用同一个 ChatGPT 账号，不会把 `previous_response_id` 或加密推理上下文迁移到另一个账号。
- 仅首轮且尚未向客户端发送任何上游帧时，上游握手 `429` 或明确的 rate/usage/quota error 才会换号，最多切换 3 次；任一下行后、客户端已发送 `response.cancel` 后和后续 turn 均不换号、不重放。首下行前的 cancel 会立即发给当前上游，并固定当前 attempt。
- 上游连接在轮间保持热态，但账号业务并发槽会释放；下一轮开始时必须重新通过该账号的额度、并发和 RPM 检查。
- 连接或上游状态无法安全恢复时会要求客户端重新连接，不会盲目重放可能已经产生工具副作用的 turn。
- HTTP Responses 路径继续保留；未使用 WebSocket 的 Codex CLI、OpenCode 和 CCS 配置不受影响。
