# HTTP Responses 分阶段耗时

开启 `SHARESUB_GATEWAY_TIMING_ENABLED=true` 后，HTTP `/responses`、`/v1/responses` 及共用该处理器的 Responses/compact 路由，在请求结束时最多输出一条 `gateway request timing` JSON 日志。不新增数据库写入，不记录正文、请求头、密钥、邮箱、代理地址或原始错误文本。WebSocket、图片、搜索及模型列表不在本次范围内。

默认开启，可通过显式设置 `SHARESUB_GATEWAY_TIMING_ENABLED=false` 关闭；正常快速请求每 100 次记录一次，耗时达到 30 秒或任何尝试的指标状态码达到 400 的请求始终保留。配置分别为 `SHARESUB_GATEWAY_TIMING_SAMPLE_EVERY` 和 `SHARESUB_GATEWAY_TIMING_SLOW_THRESHOLD`。设置采样间隔为 0 只保留慢请求和错误，为 1 保留全部。开关通过应用启动配置生效，调整配置需要重新启动应用；生产变更需另行授权。

升级注意：应用、Compose 和示例配置的默认值均为 `true`，但不会覆盖部署环境中已经显式设置的 `false`。如果现有 `.env`、宿主环境变量或 Compose override 保留了 `SHARESUB_GATEWAY_TIMING_ENABLED=false`，仅更新镜像或默认值不会开启诊断；需在后续授权的部署中将该显式配置改为 `true` 或移除，并重新创建 API 容器。

## 时间口径

- `total_ms` 从通过鉴权和请求准入后、读取请求体之前开始，到处理器返回之前结束，包含请求体读取、预处理、重试等待、上游处理和本地响应写入；不等于用户端到端延迟。
- `body_read_ms` 和 `body_bytes` 是客户端请求体读取耗时和已读取字节数，读取失败时字节数可能为部分正文。
- `request_id` 是网关关联 ID，`metric_request_id` 是最后一次记录的指标 ID。`plan_id`、`api_key_id`、`account_id` 是内部 ID，绝不是 API Key 值。文本关联 ID 限长并移除控制字符。
- `last_metric_status` 是最后一次指标状态，不承诺等于已经发送到客户端的 HTTP 状态。`had_error` 表示任意尝试记录过错误，即使后续重试成功仍保留日志。
- 顶层 `plan_id`、`api_key_id`、`account_id` 同样来自最后一次指标。每个已记录结果的 attempt 独立保存 `metric_request_id`、`plan_id`、`api_key_id`、`account_id` 和 `metric_status`，用于关联跨账号/跨 plan 重试；尚未记录结果的 attempt 不输出这些字段。转发前的策略错误只更新请求级结果，不覆盖已经结束的 attempt。
- `attempt_count` 是实际调用上游 HTTP 客户端的次数；`attempts` 最多保留前 16 次详细记录，以限制日志大小。
- 每个 attempt 的 `start_ms` 相对整个请求计时起点；其余 `*_ms` 是相对该 attempt 开始的时间点，不是可直接相加的阶段时长。请求预处理发生在 attempt 之前，可通过时间点间隙观察。
- `request_bytes` 为经过现有规范化和指纹处理后的上游请求体字节数，仅获取已有 byte slice 长度，不复制正文。
- `get_conn_ms` / `got_conn_ms` 表示获取连接的开始/结束，差值包含连接池等待、DNS、拨号、TLS 等；`connection_reused` 表示连接复用。DNS、TLS 事件仅在实际发生时出现。
- `request_written_ms` 是 HTTP 客户端成功写完请求的时间点，不保证上游应用已经完整接收正文。与 `got_conn_ms` 的差值用于估计本地发送阶段，但不等于纯网络传输时间。
- `first_response_byte_ms` 是收到响应头首字节的时间；`response_headers_ms` 是 HTTP 客户端返回响应头的时间。
- `first_sse_line_ms` 是现有 SSE 解析器读到首个非空长度行的时间，可能只是注释或协议进度；`first_content_ms` 是首次读到已有协议逻辑认可的文本、工具参数等内容的时间，不代表客户端屏幕已显示文字。
- 流式转发可能在首内容之前向下游发送结构事件，同步 Write/Flush 阻塞会推迟后续上游读取。`downstream_before_content_ms` 单独累计这段下游写入/Flush耗时（包括写入失败的耗时）；首内容出现后不再计时。若始终没有内容，则记录结束前这类写入的累计耗时。
- `first_content_excluding_downstream_ms` 仅在出现首内容时输出，等于首内容读取时间减去上述下游耗时，使用未取整的 duration 相减后转换为毫秒。它只消除了本地同步写入造成的暂停，不等于上游真实生成或到达时间：上游生成/网络传输可能与下游写入重叠。
- 没发生的事件不输出，不能把缺失当成耗时 0。非 SSE 响应不会有 SSE/内容事件。中断和连接失败也可能只有部分阶段。
- HTTP Transport 内部重试可能多次调用跟踪回调，记录各事件最后一次时间点；`write_callbacks > 1` 时不要按单次连接解释时间差。回调通过互斥锁保护，不进行日志输出或正文处理。

## 排查

在慢请求日志中先按 attempt 的账号/plan/指标请求 ID 关联记录，再对齐服务商相同时间窗口的出口监控。`request_written_ms - got_conn_ms` 很高且出口饱和，支持发送阶段瓶颈。首内容读取很晚时，先查看 `downstream_before_content_ms`；如果主要耗时来自该字段，应检查下游代理、出口及客户端接收情况，不能判为上游生成慢。排除本地发送和下游写入等待后，剩余时间仍包含上游处理及响应链路，不能直接当作 OpenAI 纯计算时间。请求写入、响应接收和下游写入可以重叠，负差值或多次回调不能机械归因。

日志写入沿用现有同步 slog handler，没有额外网络上报。采样限制正常日志量，但故障集中爆发时错误日志仍会增加；日志开销取决于运行环境，应保留容器日志轮转配置。诊断日志只在结束时输出，进行中的挂起请求要等结束后才可见，历史请求无法补录。

## 验证

在 `backend` 下运行 `go test ./internal/config ./internal/openai ./internal/httpapi`、`go test -race ./internal/openai ./internal/httpapi`，并使用 `go test ./internal/openai -run '^$' -bench RequestTiming -benchmem` 比较关闭、开启未采样和开启全量日志的本地微基准。微基准不代表生产链路的绝对延迟。
