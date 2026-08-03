# HTTP API

## 通用约定

- 管理接口使用 `Authorization: Bearer ss_session_...`。
- 网关接口使用 `Authorization: Bearer sk-sharesub-...`。
- JSON 请求必须包含一个完整对象，未知字段会返回 `400 invalid_json`。
- 份额使用整数基点：`10000` 表示 100%，`2500` 表示 25%。
- 成功响应直接返回对应 JSON 对象或数组，不额外包裹 `data` 字段。
- 错误响应结构固定为：

```json
{
  "error": {
    "code": "invalid_input",
    "message": "invalid input"
  }
}
```

## 健康检查

| 方法 | 路径 | 鉴权 | 用途 |
|---|---|---|---|
| `GET` | `/health` | 无 | 返回 `{"status":"ok"}` |

## 身份与会话

| 方法 | 路径 | 鉴权 | 请求体 | 用途 |
|---|---|---|---|---|
| `POST` | `/api/auth/register` | 无 | `username`, `email`, `password` | 注册并创建会话 |
| `POST` | `/api/auth/login` | 无 | `email`, `password` | 登录并创建会话 |
| `POST` | `/api/auth/logout` | 登录 Token | 无 | 注销当前会话 |
| `GET` | `/api/me` | 登录 Token | 无 | 获取当前用户 |
| `PATCH` | `/api/me` | 登录 Token | `username` | 修改唯一用户名 |
| `PUT` | `/api/me/avatar` | 登录 Token | `multipart/form-data` 的 `avatar` 文件 | 上传或替换头像，支持 PNG、JPEG、WebP，最大 2 MiB |
| `DELETE` | `/api/me/avatar` | 登录 Token | 无 | 移除头像 |
| `GET` | `/api/users/{userID}/avatar` | 无 | 无 | 读取用户头像二进制 |

注册和登录成功后返回 `user` 与完整的 `ss_session_...` Token。用户结构固定包含 `avatar_url`；未配置头像时为空字符串，配置后为带版本参数的站内图片地址。

## 个人仪表盘

| 方法 | 路径 | 鉴权 | 查询参数 | 用途 |
|---|---|---|---|---|
| `GET` | `/api/dashboard` | 登录 Token | `timezone` | 获取当前用户的 Token 与性能汇总及最近 24 小时趋势 |

`timezone` 必须是有效的 IANA 时区名称，例如 `Asia/Shanghai`。响应包含 `today_tokens`、`total_tokens`、`performance` 和固定 24 个小时桶的 `trend`。所有数据只聚合当前用户作为 Plan 成员实际发起的请求；“今日”边界按请求指定的时区计算。

`today_tokens` 和 `total_tokens` 包含 `input_tokens`、`output_tokens`、`cached_tokens` 与 `total_tokens`，其中总 Token 为 Input 与 Output 之和，Cached 是 Input 的子集。`performance` 包含今日请求数、成功率、最近一分钟 RPM/TPM、今日平均 TTFT、今日平均总耗时和今日实际使用的 Plan 数。

## OpenAI 账号

| 方法 | 路径 | 鉴权 | 请求体 | 用途 |
|---|---|---|---|---|
| `GET` | `/api/accounts` | 登录 Token | 无 | 列出当前用户拥有的 OpenAI 账号 |
| `POST` | `/api/accounts/openai/oauth/start` | 登录 Token | 无 | 创建带 PKCE 的 OAuth 授权流程 |
| `POST` | `/api/accounts/openai/oauth/complete` | 登录 Token | `state`, `code`, `config` | 交换授权码、保存账号与网关配置 |
| `PATCH` | `/api/accounts/{accountID}` | 登录 Token | 账号配置字段 | 账号所有者修改配置 |
| `POST` | `/api/accounts/{accountID}/oauth/start` | 登录 Token | 无 | 为指定账号开始重新授权 |
| `POST` | `/api/accounts/{accountID}/oauth/complete` | 登录 Token | `state`, `code` | 完成指定账号重新授权 |

OAuth 开始接口返回 `authorization_url` 和 `flow_id`。完成授权后，从回调 URL 中取得固定的 `state`、`code` 字段提交给完成接口。`config` 是包含以下字段的对象：

| 字段 | 约束 | 含义 |
|---|---|---|
| `name` | 1..100 个字符 | 账号显示名称 |
| `notes` | 最多 2000 个字符 | 账号备注 |
| `proxy_url` | 空字符串，或 `http://`、`https://`、`socks5://` URL | 该账号的独立出站代理 |
| `max_concurrency` | `0..100` | 最大并发请求数，`0` 表示不限制 |
| `rpm_limit` | `0..10000` | 每分钟请求上限，`0` 表示不限制 |
| `status` | `active`、`disabled`、`refresh_required` | 调度状态；OAuth 接入时固定保存为 `active` |

账号列表与 Plan 详情中的 `account` 返回 `id`、`owner_user_id`、上述配置、OpenAI 邮箱、ChatGPT Account ID、套餐类型、Token 到期时间、状态、最近错误和创建时间。OAuth access token、refresh token 以及任何密文字段永远不会进入 JSON 响应。只有账号所有者可以修改配置；Plan 的所有有效成员都能通过 Plan 详情查看该账号的完整配置。

## 共享方案、公开大厅与邀请

| 方法 | 路径 | 鉴权 | 请求体 | 用途 |
|---|---|---|---|---|
| `GET` | `/api/plans` | 登录 Token | 无 | 列出当前用户可见的方案 |
| `POST` | `/api/plans` | 登录 Token | `account_id`, `name`, `allocation_mode`, `owner_share_basis_points` | 创建方案 |
| `GET` | `/api/plans/{planID}` | 登录 Token | 无 | 获取当前成员可见的方案详情 |
| `PATCH` | `/api/plans/{planID}` | 登录 Token | `name` | 房主重命名 Plan |
| `PATCH` | `/api/plans/{planID}/status` | 登录 Token | `status` | 房主归档或恢复 Plan |
| `DELETE` | `/api/plans/{planID}` | 登录 Token | 无 | 删除已经归档的 Plan |
| `PATCH` | `/api/plans/{planID}/owner` | 登录 Token | `member_id` | 将房主身份转让给有效成员 |
| `PATCH` | `/api/plans/{planID}/account` | 登录 Token | `account_id` | 改绑房主拥有的有效 OpenAI 账号 |
| `GET` | `/api/plans/{planID}/audit-events` | 登录 Token | 无 | 获取最近 100 条 Plan 活动记录 |
| `POST` | `/api/plans/{planID}/quota/refresh` | 登录 Token | 无 | 房主主动查询并更新账号额度窗口 |
| `GET` | `/api/public-plans` | 登录 Token | 无 | 获取大厅内全部公开 Plan |
| `PATCH` | `/api/plans/{planID}/publication` | 登录 Token | `visibility`, `public_slots`, `public_share_basis_points` | 房主发布或取消公开 Plan |
| `POST` | `/api/public-plans/{planID}/applications` | 登录 Token | `message` | 申请公开 Plan 席位 |
| `PATCH` | `/api/join-applications/{applicationID}` | 登录 Token | `decision` | 房主批准或拒绝申请 |
| `POST` | `/api/plans/{planID}/invites` | 登录 Token | `share_basis_points` | 房主创建一次性邀请链接 |
| `POST` | `/api/invites/preview` | 无 | `token` | 获取邀请链接的有限预览信息 |
| `POST` | `/api/invites/accept` | 登录 Token | `token` | 接受邀请 Token |
| `DELETE` | `/api/plans/{planID}/invites/{inviteID}` | 登录 Token | 无 | 房主撤销待接受邀请 |
| `PATCH` | `/api/plans/{planID}/members/{memberID}` | 登录 Token | `share_basis_points` | 房主修改成员固定份额 |
| `DELETE` | `/api/plans/{planID}/members/{memberID}` | 登录 Token | 无 | 房主移除成员或成员主动退出 |

邀请 Token 以 `ss_invite_` 开头，有效期为 7 天，只能由一个用户领取。创建邀请固定返回 `invite` 和 `invite_url`；链接使用 `/#/invite/<token>` fragment，完整 Token 不作为独立 JSON 字段返回，也不会进入服务端请求路径。邀请不绑定邮箱，拿到链接的用户可使用任意有效账号登录或注册后领取。

`allocation_mode` 为 `fixed` 或 `shared`，创建后不可更改。`fixed` 要求房主、成员、邀请和公开席位份额为 `1..10000`；`shared` 要求这些份额字段严格为 `0`。

`visibility` 为 `private` 或 `public`。公开时席位数为 `1..100`；固定分配模式的每席份额为 `1..10000`，共享模式为 `0`。取消公开时席位数和份额都传 `0`。`decision` 为 `approve` 或 `reject`。批准会按 Plan 的额度方式立即创建成员。

固定分配模式发布公开 Plan、创建邀请和修改成员份额时，有效成员、未过期邀请和未占用公开席位的预留份额总和不能超过 `10000`。共享模式不分配个人份额。

Plan 详情的 `insights.window_usage` 按当前 OpenAI 账号实际返回的 5h/7d 窗口汇总请求数、Input/Output/Cached Token 和 `estimated_cost_micros`；`member_ranking` 为最近 7 天成员用量排行。`estimated_cost_micros` 为兼容保留的字段名，值表示账号计费（micro-USD）：按每次请求的实际模型、服务层级和同 sub2api 的 LiteLLM 模型价格表计算。

成员退出或被移除后，原 API Key 到该 Plan 的路由会被禁用。再次加入会复用原成员记录，但不会自动恢复旧路由。归档会停止网关选路、取消公开状态、撤销待处理邀请并拒绝待处理申请；只有已归档 Plan 可以永久删除。

## 通知中心

| 方法 | 路径 | 鉴权 | 请求体 | 用途 |
|---|---|---|---|---|
| `GET` | `/api/notifications` | 登录 Token | 无 | 返回 `items` 和 `unread_count` |
| `PATCH` | `/api/notifications/{notificationID}` | 登录 Token | `read` | 设置单条通知已读或未读 |
| `POST` | `/api/notifications/read-all` | 登录 Token | 无 | 全部标为已读，返回 `updated_count` |

## 用户 API Key

| 方法 | 路径 | 鉴权 | 请求体 | 用途 |
|---|---|---|---|---|
| `POST` | `/api/keys` | 登录 Token | `name`, `strategy`, `routes` | 创建用户级 API Key |
| `PATCH` | `/api/keys/{keyID}` | 登录 Token | `name`, `strategy`, `routes` | 修改 Key 名称、策略与 Plan 路由 |
| `GET` | `/api/keys` | 登录 Token | 无 | 列出当前用户的 API Key |
| `DELETE` | `/api/keys/{keyID}` | 登录 Token | 无 | 吊销当前用户的 API Key |

`strategy` 为 `priority` 或 `balanced`。`routes` 至少包含一项，每项结构为 `plan_id`, `priority`, `enabled`；只能绑定当前用户仍是有效成员的 Plan。创建接口返回 `api_key` 元数据和完整的 `sk-sharesub-...` 密钥，列表接口只返回密钥前缀。

## Codex 网关

| 方法 | 路径 | 鉴权 | 用途 |
|---|---|---|---|
| `GET` | `/v1/models` | 用户 API Key | 列出支持配置的 Codex 模型；携带 `client_version` 时返回实时 Codex manifest |
| `GET` | `/models` | 用户 API Key | 不带 `/v1` 的模型列表与 Codex manifest 兼容入口 |
| `GET` | `/backend-api/codex/models` | 用户 API Key | 转发实时 Codex models manifest |
| `POST` | `/v1/responses` | 用户 API Key | 转发 OpenAI Responses 请求 |
| `POST` | `/v1/responses/compact` | 用户 API Key | 请求远程上下文压缩 |
| `POST` | `/responses` | 用户 API Key | 不带 `/v1` 的 Responses 兼容入口 |
| `POST` | `/responses/compact` | 用户 API Key | 不带 `/v1` 的 compact 兼容入口 |
| `POST` | `/backend-api/codex/responses` | 用户 API Key | 转发 Codex Responses 请求 |
| `POST` | `/backend-api/codex/responses/compact` | 用户 API Key | Codex compact 兼容入口 |

网关请求体上限为 32 MiB。`priority` 按数字从小到大选路，并在请求发出前跳过不可用路由；`balanced` 在固定分配模式按当前成员消耗占个人份额的比例选择，在共享模式按账号总额度使用比例选择。Responses 上游明确返回 `429` 或 `529` 时最多切换 3 个账号；models manifest 在连接失败或上游返回 `401`、`429`、`5xx` 时最多切换 3 个账号。

固定分配模式的候选成员在 5 小时或 7 天窗口中达到个人份额后不可用；共享模式不限制个人用量，但账号任一有效窗口达到 100% 后不可用。所有候选都不可用时返回 `429 quota_exhausted`；没有有效路由时返回 `503 no_route_available`。只有完整额度响应头会更新额度快照；额度头缺失不会篡改或拦截上游成功响应。指标记录状态码、TTFT、总耗时、终止事件中的 Input/Output/Cached Token 和关联成员，不记录请求或响应内容。

## 常见错误码

| HTTP 状态 | 错误码 | 含义 |
|---|---|---|
| `400` | `invalid_json` | JSON 格式错误、包含未知字段或不是单个对象 |
| `400` | `invalid_input` | 输入值不符合业务约束 |
| `401` | `unauthorized` | Token 缺失、格式错误、过期或已吊销 |
| `403` | `forbidden` | 当前用户无权执行该操作 |
| `404` | `not_found` | 目标不存在或不可见 |
| `409` | `conflict` | 数据状态冲突 |
| `409` | `share_exceeded` | 方案已分配份额超过 100% |
| `413` | `request_too_large` | 网关请求体超过 32 MiB |
| `429` | `quota_exhausted` | 固定分配的成员份额或共享使用的账号总额度已耗尽 |
| `429` | `account_concurrency_limited` | 当前账号已达到最大并发请求数 |
| `429` | `account_rate_limited` | 当前账号已达到 RPM 上限 |
| `409` | `public_plan_full` | 公开 Plan 的固定席位已满 |
| `502` | `upstream_unavailable` | OpenAI 上游请求失败 |
| `503` | `account_unavailable` | 绑定账号不可用或刷新 Token 失败 |
| `503` | `no_route_available` | API Key 没有仍然有效的 Plan 路由 |
