# ShareSub

ShareSub 是一个仅支持 OpenAI Codex 的账号共享平台。房主可以先创建 Plan、探索协作设置，再在 Plan 内接入或选择自己的 OpenAI 账号；额度方式可以按成员固定分配，也可以让所有成员共享账号总额度。Plan 可通过一次性链接邀请成员，也可以预留公开席位后发布到大厅。用户申请公开席位后，由房主批准或拒绝。

这是一个从零开发的独立项目。Sub2API 仅用于参考 OpenAI 协议实现，不是 ShareSub 的运行时依赖或源码依赖。

## 当前能力与边界

- 仅支持 OpenAI Codex OAuth 账号，不支持 Anthropic、Gemini、Bedrock 等其他平台。
- Plan 分为公开和私密两种；公开 Plan 会展示在大厅，但申请必须由房主审批。
- 一个共享方案最多绑定一个房主拥有的 OpenAI 账号；未绑定时可以配置 Plan、公开招募和审批成员，但不能配置 API Key 路由或转发请求。
- OpenAI 账号支持名称、备注、启停状态、独立代理、最大并发和 RPM 上限；OAuth 接入与刷新会同步当前付费订阅的有效期，只有账号所有者可编辑配置，Plan 的所有成员可查看完整配置。
- API Key 归用户所有，一个 Key 可以绑定用户已加入的多个 Plan。
- API Key 支持按优先级故障转移，或按可用额度均衡选路。
- OpenAI 网关支持流式和非流式 Responses、远程 compact、Codex 独立联网检索、模型列表，以及兼容 OpenAI Images 的图片生成和编辑接口。
- Plan 创建时选择额度方式：`fixed` 按成员固定分配份额，`shared` 由所有成员共享账号总额度；共享模式可由房主单向转换为固定分配，转换后不可改回共享模式。
- 固定分配 Plan 中，房主、成员、待接受邀请和公开席位的份额可以为 0%；0% 成员仍可查看 Plan，但不能通过该 Plan 发起请求。所有预留份额之和不能超过 100%。共享使用 Plan 不设置个人份额或个人额度上限。
- 公开 Plan 上架时必须预留席位；固定分配模式需设置每席份额（允许为 `0`），共享模式的每席份额固定为 `0`。
- 额度同时按 Codex 5 小时窗口和 7 天窗口执行。
- 共享 Plan 转为固定分配时，未分配成员、待接受邀请和公开席位的份额保持 0%；转换前当前窗口内的成员用量继续计入转换后的个人额度。
- 登录后默认进入个人仪表盘，可查看今日/累计 Token、RPM/TPM、平均响应、成功率和最近 24 小时趋势。
- 未登录用户可访问产品首页、用户协议、隐私政策和可接受使用规范；注册必须明确接受服务端当前协议版本，并由服务端记录接受时间。
- 新注册账户必须先通过腾讯云 SES 事务邮件验证邮箱，验证完成后才会创建登录会话。
- Plan 成员可以查看账号额度、每位成员的额度消耗、最近 30 分钟/6 小时/12 小时/24 小时聚合性能和最近 7 天请求指标；系统不保存请求正文。
- 不包含通用供应商、渠道、分组、调度器、计费和支付抽象。

## 技术栈

- 后端：Go 1.25、`net/http`、PostgreSQL 17、pgx
- 前端：Vue 3、TypeScript、Vite、pnpm
- 部署：Docker Compose、Nginx

## 目录结构

```text
backend/      Go API、OpenAI OAuth、Codex 网关、PostgreSQL 数据访问
frontend/     Vue 3 管理界面
docs/         架构与 HTTP API 文档
deploy/       Docker Compose 部署配置
```

## 快速启动（Docker）

前置要求：已安装 Docker Engine 和 Docker Compose Plugin。

在项目根目录执行：

```bash
cp .env.example .env
openssl rand -base64 32
openssl rand -base64 32
```

将两次命令生成的值分别填入 `.env`：

```dotenv
SHARESUB_TOKEN_PEPPER=第一段随机值
SHARESUB_CREDENTIAL_KEY=第二段随机值
```

两个值必须分别生成，Base64 解码后必须恰好为 32 字节。设置完成后启动全部服务：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml pull
docker compose --env-file .env -f deploy/docker-compose.yml up -d --no-build
docker compose --env-file .env -f deploy/docker-compose.yml ps
```

访问 `http://127.0.0.1:8081`。API 健康检查地址为 `http://127.0.0.1:8081/health`。Compose 项目名固定为 `sharesub`，不会与通常从 `deploy` 目录启动的 sub2api 共用项目标识。

查看日志或停止服务：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml logs -f api web
docker compose --env-file .env -f deploy/docker-compose.yml down
```

`down` 不会删除 PostgreSQL 数据卷。不要执行 `down -v`，除非确定要删除全部数据库数据。

## 本地开发

### 一键启动

本机已经安装 Go、Node.js、pnpm、Docker 和前端依赖时，可在项目根目录执行：

```bash
make dev
```

第一次执行时，Makefile 会根据 `.env.example` 创建 `.env` 并生成两个独立随机密钥，然后启动 PostgreSQL、后端和前端。命令会留在前台持续输出日志，按 `Ctrl-C` 可停止前端和后端，PostgreSQL 会继续保留。Makefile 不会自动安装开发工具或下载前端依赖；检查发现缺失时会直接退出并提示。

常用管理命令：

```bash
make status   # 查看 PostgreSQL、后端和前端状态
make logs     # 持续查看后端和前端日志
make stop     # 停止后端和前端，保留 PostgreSQL
make down     # 停止全部服务，保留数据库数据卷
make restart  # 重启全部本地开发服务
make install-hooks  # 为当前 clone 启用提交前结构检查
make quality  # 执行规模、格式、静态分析、测试、类型和构建门禁
make test     # 执行后端测试、静态检查和前端构建检查
```

代码规模、模块边界、数据治理和重构要求见 [`CONTRIBUTING.md`](CONTRIBUTING.md) 与 [`docs/data-governance.md`](docs/data-governance.md)。

### 前置要求

- Go 1.25 或更高版本
- Node.js 22 或更高版本
- pnpm
- PostgreSQL 17，或可用的 Docker 环境

### 1. 准备环境变量

```bash
cp .env.example .env
openssl rand -base64 32
openssl rand -base64 32
```

把生成的两个值分别填入 `.env` 的 `SHARESUB_TOKEN_PEPPER` 和 `SHARESUB_CREDENTIAL_KEY`。

主要配置项：

| 配置项 | 用途 | 本地默认值 |
|---|---|---|
| `SHARESUB_HTTP_ADDR` | 后端监听地址 | `127.0.0.1:8080` |
| `SHARESUB_DATABASE_URL` | PostgreSQL 连接地址 | `postgres://sharesub:sharesub@127.0.0.1:5432/sharesub?sslmode=disable` |
| `SHARESUB_PUBLIC_URL` | 平台对外地址 | `http://127.0.0.1:5173` |
| `SHARESUB_DOCKER_PUBLIC_URL` | Docker 部署的平台对外地址 | `http://127.0.0.1:8081` |
| `SHARESUB_WEB_BIND_HOST` | Docker Web 宿主机监听地址 | `127.0.0.1` |
| `SHARESUB_WEB_PORT` | Docker Web 宿主机端口 | `8081` |
| `SHARESUB_DATABASE_PORT` | 本地开发 PostgreSQL 宿主机端口 | `5432` |
| `SHARESUB_SESSION_TTL` | 登录会话有效期 | `720h` |
| `SHARESUB_EMAIL_DELIVERY_PROVIDER` | 注册邮件服务商；启用邮箱注册时填写 `tencent_ses` | 留空 |
| `SHARESUB_TENCENT_SES_SECRET_ID` | 具有 SES 发信权限的腾讯云 CAM SecretId | 留空 |
| `SHARESUB_TENCENT_SES_SECRET_KEY` | 对应 CAM SecretKey；只能保存在服务端环境变量中 | 留空 |
| `SHARESUB_TENCENT_SES_REGION` | 腾讯云 SES API 地域 | `ap-hongkong` |
| `SHARESUB_TENCENT_SES_FROM_EMAIL` | SES 已验证发信域名下的发件地址 | `no-reply@notify.example.com` |
| `SHARESUB_TENCENT_SES_FROM_NAME` | 邮件中显示的发件人名称 | `ShareSub` |
| `SHARESUB_TENCENT_SES_TEMPLATE_ID` | 腾讯云审核通过的邮箱验证模板 ID | 留空 |
| `SHARESUB_EMAIL_VERIFICATION_TTL` | 一次性邮箱验证链接有效期 | `1h` |
| `SHARESUB_EMAIL_RESEND_COOLDOWN` | 同一账户重新发送验证邮件的最短间隔 | `1m` |
| `SHARESUB_CLEANUP_INTERVAL` | 过期资源清理周期 | `6h` |
| `SHARESUB_GATEWAY_METRIC_RETENTION` | 网关请求明细保留期；至少 7 天，清理边界按 UTC 整日对齐，清理前会汇总 | `2160h`（90 天） |
| `SHARESUB_GATEWAY_MAX_REQUESTS_PER_MINUTE_PER_API_KEY` | 每个 API Key 的网关请求分钟上限，用于抑制突发重试风暴 | `300` |
| `SHARESUB_GATEWAY_FIRST_OUTPUT_TIMEOUT` | HTTP Responses 等待上游首个有效语义输出的最长时间 | `2m` |
| `SHARESUB_AUDIT_EVENT_RETENTION` | 审计记录保留期 | `8760h`（365 天） |
| `SHARESUB_READ_NOTIFICATION_RETENTION` | 已读通知保留期；未读通知不自动删除 | `2160h`（90 天） |
| `SHARESUB_TERMINAL_RECORD_RETENTION` | 已结束邀请、申请及撤销 Key 保留期 | `2160h`（90 天） |
| `SHARESUB_TOKEN_REFRESH_ENABLED` | 是否启用 OpenAI OAuth Token 后台自动刷新 | `true` |
| `SHARESUB_TOKEN_REFRESH_INTERVAL` | 后台扫描即将到期 Token 的周期 | `5m` |
| `SHARESUB_TOKEN_REFRESH_BEFORE_EXPIRY` | 提前多久刷新 access token | `30m` |
| `SHARESUB_TOKEN_REFRESH_BATCH_SIZE` | 每轮最多扫描的账号数 | `200` |
| `SHARESUB_TOKEN_REFRESH_CONCURRENCY` | 单实例后台刷新并发数 | `4` |
| `SHARESUB_TOKEN_REFRESH_MAX_RETRIES` | 后台刷新失败最大尝试次数 | `3` |
| `SHARESUB_RESPONSES_WS_FIRST_MESSAGE_TIMEOUT` | WebSocket Upgrade 后等待首个 `response.create` 的最长时间 | `30s` |
| `SHARESUB_RESPONSES_WS_INTER_TURN_IDLE_TIMEOUT` | WebSocket 两个 turn 之间允许的最长空闲时间 | `5m` |
| `SHARESUB_RESPONSES_WS_MAX_SESSION_DURATION` | 单条 Responses WebSocket 会话最长持续时间 | `1h` |
| `SHARESUB_RESPONSES_WS_MAX_CONNECTIONS_PER_API_KEY` | 单实例中每个用户 API Key 最多同时打开的 Responses WebSocket 数 | `64` |
| `SHARESUB_RESPONSES_WS_DIAL_TIMEOUT` | 建立上游 Responses WebSocket 的超时时间 | `10s` |
| `SHARESUB_RESPONSES_WS_READ_TIMEOUT` | 活跃 turn 等待上游下一帧的超时时间 | `15m` |
| `SHARESUB_RESPONSES_WS_WRITE_TIMEOUT` | 写入任一 WebSocket 帧的超时时间 | `2m` |
| `SHARESUB_RESPONSES_WS_UPSTREAM_DRAIN_TIMEOUT` | 客户端断开后继续读取上游终态 usage 的最长时间 | `1.2s` |
| `SHARESUB_RESPONSES_WS_CLIENT_READ_LIMIT_BYTES` | 客户端 WebSocket 单条消息上限 | `67108864`（64 MiB） |
| `SHARESUB_RESPONSES_WS_UPSTREAM_READ_LIMIT_BYTES` | 上游 WebSocket 单条消息上限 | `16777216`（16 MiB） |
| `SHARESUB_OAUTH_REDIRECT_URI` | OpenAI OAuth 回调地址 | `http://localhost:1455/auth/callback` |
| `SHARESUB_OUTBOUND_PROXY` | OpenAI OAuth 和网关出站代理 | 留空，表示直连 |
| `SHARESUB_TOKEN_PEPPER` | Token 哈希密钥 | 必填，32 字节 Base64 |
| `SHARESUB_CREDENTIAL_KEY` | OAuth 凭据加密密钥 | 必填，32 字节 Base64 |

生产 Compose 默认将 API、Web、PostgreSQL 的内存限制为 `1 GiB`、`256 MiB`、`1 GiB`，CPU 限制为 `2`、`0.5`、`2`；可通过对应的 `SHARESUB_*_MEMORY_LIMIT` 和 `SHARESUB_*_CPU_LIMIT` 环境变量调整。容器日志单文件最多 20 MiB、保留 5 份。部署脚本将数据库备份限制为最近 14 份且不超过 30 天，并为 API/Web 各保留最近 3 个发布及回滚镜像。

启用注册邮箱验证时，腾讯云 SES 模板变量必须包含 `token`，验证链接应使用 `SHARESUB_PUBLIC_URL` 对应站点的 `/verify-email#token={{token}}`。CAM 子账号只需授予 SES 发信所需权限，不要把腾讯云控制台登录密码写入 `.env`。

### 2. 启动 PostgreSQL

可以只启动 Compose 中的 PostgreSQL：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/docker-compose.dev.yml up -d postgres
```

开发覆盖配置会让 PostgreSQL 监听 `127.0.0.1:5432`。该端口已被占用时，同时修改 `.env` 中的 `SHARESUB_DATABASE_PORT` 和 `SHARESUB_DATABASE_URL`。生产部署不要加载 `docker-compose.dev.yml`，PostgreSQL 只在 Compose 内部网络监听。后端启动时会自动执行尚未应用的数据库迁移。

### 3. 启动后端

Go 程序不会自动读取 `.env`，需要先把文件中的变量导入当前 Shell：

```bash
cd backend
set -a
source ../.env
set +a
go run ./cmd/api
```

后端默认监听 `http://127.0.0.1:8080`。

### 4. 启动前端

另开一个终端：

```bash
cd frontend
pnpm install
pnpm dev
```

前端开发地址是 `http://127.0.0.1:5173`。Vite 会自动将 `/api`、`/v1`、`/responses` 和 `/backend-api` 转发到本地后端。

## 基本使用流程

1. 阅读并同意用户协议、隐私政策和可接受使用规范，使用唯一用户名、邮箱和密码注册并登录 ShareSub。用户名可在“个人设置”中修改。
2. 在“我的 Plans”中创建共享方案并选择固定分配或共享使用；固定分配模式还需设置自己的初始份额。
3. 在已创建 Plan 的“账号配置”中选择尚未绑定的已有账号，或点击“接入新账号并绑定”。
4. 若接入新账号，设置名称、备注、独立代理与网关限速策略，打开系统生成的 OpenAI 授权地址并完成授权。
5. 授权后地址栏会跳转到 `http://localhost:1455/auth/callback?...`。即使本地页面无法打开，也可复制完整地址并粘贴回 ShareSub 的“回调地址”输入框；账号接入成功后会直接绑定到当前 Plan。
6. 私密协作：房主在 Plan 的“成员”Tab 创建 7 天内有效的一次性邀请链接；固定分配模式同时设置成员份额。拿到链接的用户使用任意邮箱登录或注册后会自动加入 Plan。
7. 公开招募：房主设置公开席位数并上架；固定分配模式同时设置每席份额。用户在大厅申请，房主批准后申请人立即成为成员。
8. 用户在“API Keys”中创建 `sk-sharesub-...` Key，并为它选择一个或多个自己已加入的 Plan。完整密钥加密保存，可随时生成 Codex CLI/OpenCode 配置或导入到 CCS。
9. 用户可在默认仪表盘查看自己的今日/累计 Token、性能和最近 24 小时趋势；Plan 的所有成员还可以查看所绑定账号的完整配置、账号与成员的 5h/7d 消耗、可选时间段性能和最近 7 天成员用量排行。

份额使用基点表示：`10000` 代表 100%，`2500` 代表 25%。

## 调用 Codex 网关

成员密钥可独立配置 Fast/Flex 规则；Plan 绑定账号的规则优先，账号规则为空、未命中或透传时才执行 Key 规则。“强制 Fast”会在请求未携带 `service_tier` 时主动写入 ChatGPT Codex 上游兼容值 `priority`；入站的 `fast` 与 `priority` 按同一 Fast 模式匹配。成员密钥可调用以下入口：

- `GET /v1/models`
- `GET /models`
- `GET /backend-api/codex/models`
- `GET /v1/responses`（Responses WebSocket v2 Upgrade）
- `GET /responses`（Responses WebSocket v2 Upgrade）
- `GET /backend-api/codex/responses`（Responses WebSocket v2 Upgrade）
- `POST /v1/responses`
- `POST /v1/responses/compact`
- `POST /responses`
- `POST /responses/compact`
- `POST /backend-api/codex/responses`
- `POST /backend-api/codex/responses/compact`
- `POST /v1/alpha/search`
- `POST /alpha/search`
- `POST /backend-api/codex/alpha/search`
- `POST /v1/images/generations`
- `POST /v1/images/edits`
- `POST /images/generations`
- `POST /images/edits`

推荐普通 OpenAI 客户端使用 `/v1` 开头的标准入口；Codex 会通过 `/models?client_version=...` 获取所选 Plan 账号的实时模型 manifest，其余路径用于兼容已有 Codex/sub2api 客户端配置。

示例：

```bash
curl https://sharesub.example.com/v1/responses \
  -H 'Authorization: Bearer sk-sharesub-替换为成员密钥' \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  --data '{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"stream":true,"store":false}'
```

网关会按 Key 的配置从仍然有效的 Plan 中选路：`priority` 按数字从小到大选择，当前路由在请求发出前不可用或达到账号并发/RPM 上限时尝试下一条；`balanced` 在固定分配模式按成员消耗占个人份额的比例选择，在共享模式按账号总额度使用比例选择。固定分配模式会分别在 5 小时和 7 天窗口内，先扣除账号本次绑定 Plan 时已经消耗的额度，再按同期成员请求费用占比分摊剩余用量；任一窗口达到成员份额后停止使用该 Plan。固定份额为 0% 的成员不参与选路；共享 Plan 不执行个人额度限制，但账号任一有效窗口达到 100% 后会停止路由。账号独立代理优先于 `SHARESUB_OUTBOUND_PROXY`，代理请求失败时不会回退直连。只有上游明确以 `429` 或 `529` 拒绝执行时，网关才会切换到下一个账号，最多切换 3 次；连接错误、超时和其他状态不会重试，避免重复执行。系统不把请求或响应正文写入数据库或性能记录。

OAuth 请求会移除 ChatGPT 内部接口不支持的顶层字段并强制 `store=false`。非流式请求由网关把上游 SSE 终止响应还原成 JSON；流式上游异常关闭时会发出标准 `response.failed` 终止事件。三个 Responses 入口也支持 WebSocket v2：连接固定账号并复用一条上游 WebSocket，账号并发与 RPM 按 turn 获取和释放，每个终止 turn 独立记录 usage。仅首轮在尚未向客户端发送任何上游帧时，上游握手 `429` 或明确的 rate/usage/quota error 才允许换号，最多切换 3 次；任一下行帧之后、客户端已发送 `response.cancel` 后和后续 turn 均不换号、不重放。WebSocket 上游同样优先使用账号独立代理，账号未配置代理时使用 `SHARESUB_OUTBOUND_PROXY`。完整兼容范围见 [转发兼容说明](docs/forwarding-compatibility.md)。

Codex 独立联网检索通过三个 `alpha/search` 兼容入口转发到 ChatGPT SearchClient 上游。该请求使用独立 JSON 协议，不携带 Responses 专用的 Beta 与会话头；每个成功的 2xx 搜索按一次 Web Search 计入用量与成本。

Images 接口支持 `gpt-image-1`、`gpt-image-1.5`、`gpt-image-2`，生成请求使用 JSON，编辑请求支持 JSON 图片 URL 或 multipart 文件与 mask。网关把请求转换为 ChatGPT Responses 的 `image_generation` 工具调用，并将固定的图片事件转换回 OpenAI Images JSON 或 SSE 响应。

## 使用 Docker 部署到服务器

### 1. 准备服务器

安装 Docker Engine 和 Docker Compose Plugin，并通过 Git 克隆或其他方式把项目放到服务器。以下命令都在项目根目录执行。

创建生产环境文件：

```bash
cp .env.example .env
openssl rand -base64 32
openssl rand -base64 32
chmod 600 .env
```

编辑 `.env`，至少修改：

```dotenv
SHARESUB_DOCKER_PUBLIC_URL=https://share.example.com
SHARESUB_OAUTH_REDIRECT_URI=http://localhost:1455/auth/callback
SHARESUB_OUTBOUND_PROXY=
SHARESUB_TOKEN_PEPPER=独立生成的第一段随机值
SHARESUB_CREDENTIAL_KEY=独立生成的第二段随机值
```

不要在服务已经产生数据后随意更换 `SHARESUB_TOKEN_PEPPER` 或 `SHARESUB_CREDENTIAL_KEY`。更换后，已有登录 Token、用户 API Key 或已加密的 OpenAI 凭据将无法继续使用。

首次启动且数据库中不存在管理员时，后端会自动创建 `admin@underelay.com`，并且只在创建成功的那次 API 日志中输出临时密码。使用 `docker compose logs api | grep "bootstrap admin"` 查看；首次登录必须设置新密码后才能访问其他功能。后续启动不会重新生成或再次输出密码。遗失密码时可在 API 容器中执行 `sharesub-admin reset-password`，命令会撤销该管理员的现有会话并输出新的临时密码。

管理员沿用普通登录入口，角色持久保存在数据库中，不提供网页提权接口。后台可以查看平台概览、API 容器 CPU/内存、PostgreSQL 连接池、Go 协程和后台任务状态，以及用户、OpenAI 账号、Plan 和 API Key，支持禁用/恢复用户、启用/禁用账号及吊销 Key。后台响应不会返回 OAuth 密文、代理密文或 API Key 原文。

如果服务器不能直连 OpenAI，可将 `SHARESUB_OUTBOUND_PROXY` 设置为实际可用的 `http://`、`https://` 或 `socks5://` 代理地址。Docker 部署时，`127.0.0.1` 指向 API 容器自身，不能用它表示宿主机代理；应使用容器可访问的代理地址或把代理作为 Compose 服务运行。

### 2. 启动服务

```bash
docker compose --env-file .env -f deploy/docker-compose.yml pull
docker compose --env-file .env -f deploy/docker-compose.yml up -d --no-build
docker compose --env-file .env -f deploy/docker-compose.yml ps
docker compose --env-file .env -f deploy/docker-compose.yml logs -f api web
```

生产 Compose 只使用 GHCR 中已经构建好的镜像，不在服务器编译 Go 或前端资源。私有镜像需要先在服务器执行 `docker login ghcr.io`；完整的 GitHub Actions、GHCR 和服务器配置见 [部署工作流](docs/deployment.md)。

当前 Compose 将 Web 仅绑定到服务器的 `127.0.0.1:8081`，并且不把 PostgreSQL 映射到宿主机。生产环境应使用宿主机上的 Nginx 或 Caddy 将独立域名反向代理到 `127.0.0.1:8081`，并配置 HTTPS。

### 与 sub2api 共存

默认配置已经隔离以下资源：

| 资源 | sub2api 默认值 | ShareSub 默认值 |
|---|---|---|
| Compose 项目名 | 通常为 `deploy` | 固定为 `sharesub` |
| 宿主机 Web 端口 | `0.0.0.0:8080` | `127.0.0.1:8081` |
| PostgreSQL 宿主机端口 | 推荐配置不映射 | 生产配置不映射 |
| 数据库与账号 | `sub2api` | 独立的 `sharesub` |
| Docker 网络 | sub2api 专用网络 | `sharesub_default` |

两个服务应使用不同子域名，例如 `api.example.com` 指向 sub2api，`share.example.com` 指向 ShareSub。不要把它们放在同一域名的不同路径下，两边都使用 `/`、`/api` 和 `/v1` 等根路径。

如果 sub2api 已经通过宿主机 Nginx、Caddy 或其他反向代理提供 HTTPS，应在现有代理中增加 ShareSub 的虚拟主机，不要再启动第二个占用宿主机 `80/443` 的代理进程。`8081` 也被占用时，在 `.env` 修改 `SHARESUB_WEB_PORT`，并把反向代理目标改成相同端口。

部署前可以只读检查服务器现状：

```bash
docker compose ls
docker ps --format 'table {{.Names}}\t{{.Ports}}\t{{.Label "com.docker.compose.project"}}'
ss -lntp
```

旧版 ShareSub 也曾使用隐式项目名 `deploy`。先检查旧容器归属：

```bash
docker inspect deploy-postgres-1 --format '{{ index .Config.Labels "com.docker.compose.project.config_files" }}'
```

只有输出确实指向当前 ShareSub 仓库的 `deploy/docker-compose.yml` 时，才执行一次迁移：

```bash
docker compose -p deploy --env-file .env -f deploy/docker-compose.yml -f deploy/docker-compose.dev.yml down
docker compose --env-file .env -f deploy/docker-compose.yml pull
docker compose --env-file .env -f deploy/docker-compose.yml up -d --no-build
```

如果 `deploy` 属于 sub2api，不要执行第一条停止命令。数据库卷固定使用原有唯一名称 `deploy_sharesub_postgres`，切换项目名不会创建空数据库卷。本地 `make dev` 也会检测旧版 ShareSub 容器并拒绝同时启动两个 PostgreSQL 实例。

### 3. 配置 HTTPS 反向代理

宿主机 Nginx 示例：

```nginx
map $http_upgrade $sharesub_connection_upgrade {
    default upgrade;
    ''      '';
}

server {
    listen 80;
    server_name sharesub.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name sharesub.example.com;

    ssl_certificate /etc/letsencrypt/live/sharesub.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sharesub.example.com/privkey.pem;

    client_max_body_size 256m;

    location ~ ^/(v1/alpha/search|alpha/search|backend-api/codex/alpha/search)$ {
        client_max_body_size 32m;
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $sharesub_connection_upgrade;
        proxy_connect_timeout 60s;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

将示例域名和证书路径替换为实际值。流式响应路径需要关闭代理缓冲，并设置足够长的读取超时；Responses WebSocket v2 还要求外层代理透传 `Upgrade` 和 `Connection`。

### 4. 升级与回滚准备

推荐通过 GitHub Actions 手动运行 `Deploy production`。发布前，当前 `main` 的 `CI and images` 必须已经成功，将两个带完整 commit SHA 的镜像推送到 GHCR。GitHub `production` Environment 可以配置人工审批和生产 SSH Secrets。

正式版本以根目录 `VERSION` 为唯一来源，格式为不带 `v` 前缀的 SemVer。每次生产发布前必须为当前提交创建对应的 annotated tag：

```bash
version="$(./scripts/verify-version.sh)"
git tag -a "v$version" -m "ShareSub v$version"
git push origin "v$version"
```

`scripts/verify-version.sh --release` 会校验 `VERSION`、tag 名称、tag 类型和 tag 指向的提交。生产仍使用完整 commit SHA 镜像部署与回滚，SemVer tag 不代替不可变镜像标识。

生产部署只能通过 GitHub Actions 的 `Deploy production` workflow 执行。`scripts/deploy.sh` 是该 workflow 的内部部署引擎，不是开发机上的发布入口；脚本会拒绝本地直接调用。workflow 会运行完整测试、同步当前提交、拉取对应完整 commit SHA 的 API/Web 镜像、保留当前运行镜像用于回滚、备份生产数据库、更新容器并验证公网健康接口。服务器不会再执行镜像构建。拉取或备份失败时不会切换当前容器；不包含迁移的版本健康检查失败时会自动恢复上一组镜像。仓库中的 Nginx 配置发生变化时，脚本会停止并要求先人工安装配置。

检测到 `backend/migrations` 变化时，必须在手动运行 workflow 时明确启用 `allow_migrations`。

迁移版本不会自动恢复旧数据库；发布前生成的压缩备份会保存在 workflow 配置的生产部署目录下的 `backups/`。完整的新旧工作流、首次配置、回滚边界和机器资源建议见 [部署工作流](docs/deployment.md)。升级只能使用 GitHub Actions，不要绕过迁移确认、备份、不可变镜像和健康检查直接运行脚本或 Compose。

后端进程启动时会自动应用新增迁移。

### 5. 数据库备份与恢复

创建压缩格式备份：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml exec -T postgres \
  pg_dump -U sharesub -d sharesub -Fc > sharesub.dump
```

恢复会覆盖同名对象中的数据，应先停止 API 并确认备份文件无误：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml stop api
docker compose --env-file .env -f deploy/docker-compose.yml exec -T postgres \
  pg_restore -U sharesub -d sharesub --clean --if-exists < sharesub.dump
docker compose --env-file .env -f deploy/docker-compose.yml start api
```

建议同时安全备份生产 `.env`。数据库备份不包含其中的加密密钥；缺少原有 `SHARESUB_CREDENTIAL_KEY` 时，备份中的 OAuth 凭据无法解密。

## 验证

完整本地门禁：

```bash
make quality
```

后端：

```bash
cd backend
go test ./...
go vet ./...
```

前端：

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm lint
pnpm test:run
pnpm typecheck
pnpm build
pnpm bundle:check
```

Docker 配置：

```bash
docker compose --env-file .env -f deploy/docker-compose.yml config
docker compose --env-file .env -f deploy/docker-compose.yml -f deploy/docker-compose.dev.yml config
```

## 安全与风险

- OAuth access token、refresh token 和账号独立代理使用 AES-256-GCM 加密后保存；密码使用 bcrypt；登录和 API Token 仅保存带 Pepper 的 SHA-256 哈希。
- `.env`、数据库备份、邀请链接和完整 API Key 都属于敏感信息，不应提交到 Git、记录到公开日志或通过不安全渠道发送。
- 转发基于订阅的 OpenAI 访问可能与 OpenAI 的服务条款或账号政策冲突。部署和运营者需要自行评估账号、隐私、转售和所在地区的合规要求。

更多实现说明见 [架构文档](docs/architecture.md) 和 [HTTP API 文档](docs/api.md)。
