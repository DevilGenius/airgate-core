# 请求 Trace 的边界与接入

## 保存内容

Trace 开关沿用最终失败请求诊断语义，不是保存所有成功请求的审计开关。

- Core 在入口、任何协议转换之前复制客户端请求体，写入 `request.body`。未触发脱敏的正文、历史、扩展字段及 JSON 排版保持原样；触发脱敏时保留原因与原长度。
- 插件通过 `sdkgo/requesttrace` 记录实际发送的 HTTP 请求或 WebSocket 消息，写入 `attempts[].outbound_requests[].body`。客户端原文与上游协议转换后的正文分别保存，不互相覆盖。
- HTTP 状态、上游原始响应或最近读取的 SSE/WebSocket 数据事件作为辅助诊断保存；它们不能替代请求正文。
- SDK 只采集有界原始数据；图片内容、附件、认证字段、敏感请求头等由 Core 在落库前统一脱敏，详细范围见下文。
- 本地校验未发起上游请求时，Core 仍有客户端原文，但不会伪造出站请求。

## SDK 采集器

每次 `Forward` 入口调用 `requesttrace.Start(ctx, req.TraceFinalError)`，出口在所有结果策略之后调用 `capture.Finish(&outcome, err)`。应使用闭包 defer，以读取最终的 outcome 和 err。

只有主模型转发使用 `buildForwardHTTPClient` 包装 `requesttrace.Transport`。采集器从当前请求的 context 获取，不绑定在共享连接池或账号对象上。普通 `buildHTTPClient` 不采集 trace；图片上传/下载、BAS 附件上传、认证请求和辅助轮询均不进入出站 trace。`imageDownloadHTTPClient` 与 imgen 客户端也不安装 trace 传输，因此 `images_web_reverse` 使用的图片辅助链路不会覆盖主请求诊断。

可重放请求体在发送前读取独立 reader，保证后续修改原始缓冲区不会污染 trace。没有 `GetBody` 的 reader 使用随读采集，不预读或阻塞真实发送。响应也只随读取观察，不提前消耗 SSE、不新增读协程、不改变流关闭和取消行为。

WebSocket 在统一拨号入口用 `requesttrace.WrapWebSocket` 包装。它在消息读写边界采集实际字节，普通 OAuth、图片生成与其他使用同一连接封装的模式不再手动埋点。失败握手通过 `Record` / `WrapResponse` 保存。

OpenAI 插件仅在公共 `Forward` 入口读取 trace 开关，业务模式不调用专用 trace API。BAS 转换器也不再包含诊断 observer。旧的 `final_error_trace` 模块和重复脱敏实现已经移除，没有旧函数别名或双轨兼容逻辑。

## 资源与落盘边界

每次插件 attempt 最多保留 8 个出站请求（最早一个与最近七个），诊断正文总预算 48 MiB，单个 SSE 数据事件最多 16 MiB。正常大小的文本请求逐字节保存；超过预算或不可完整读取的请求省略正文并保留已知或观察到的原长度，Core 根据长度差异标记 `trace_capture_incomplete`，不将其伪装为完整正文。SDK 的 gRPC 消息上限仍然适用。

Core 负责失败 attempt 汇总、最终失败判定、统一脱敏、gzip 压缩、去重、保留期和监控页面读取。SDK 采集器不访问数据库、不决定计费或重试，也不区分 BAS、OAuth、Chat 或 Anthropic。

## 保留期与内存生命周期

trace 表不设置过期时间，不进行自动清理，永久保留直到管理员手动清空。普通监控事件的保留策略独立，不受此改动影响。升级迁移只移除 trace 的 expires_at 列及其索引，不删除历史 payload。

插件 `Finish` 无论成功失败都主动断开采集器对请求、响应和 SSE 工作缓冲区的引用，并拒绝迟到的数据。Core 同时清空 Gin 池化 context 中的 trace 引用。成功请求的 trace 不落盘，缓冲区随后由 Go GC 回收；解除引用不等于操作系统工作集立即下降。

失败请求的诊断快照仍归 Core 异步队列所有，直到编码、落库或丢弃完成。队列容量 32 条，原始正文预算 512 MiB，满载时非阻塞降级为仅记录事件。该预算不是进程内存上限：并发请求、序列化临时分配及压缩器工作区另计。压缩器复用时会先解除对输出 payload 的引用。

开启 trace 会增加正文复制、SSE 观察和失败诊断脱敏开销；关闭时走传输层快速返回。基准方法和结果见 [性能测试报告](request-trace-performance.md)。

## 指纹一致性

Core 对入口与插件出站原始头统一使用内部 traceredaction 的 xxh3-128 实现，输出 32 位小写十六进制。HeaderFingerprints 统一字段别名和优先级，多值使用 NUL 分隔且保留值顺序；落盘键名分别为 x-airgate-trace-session-id-xxh3-128、x-airgate-trace-conversation-id-xxh3-128、x-airgate-trace-x-codex-turn-state-xxh3-128。同一原始值在两侧可直接比较。

已落盘的历史 trace 不重写：旧 SHA256 摘要无法反推原值，且修改 payload 会改变内容 hash。统一规则对更新后的 Core 与插件生成的新 trace 生效。

## 统一脱敏策略

规则入口为 `backend/internal/app/monitor/traceredaction/redaction_policy.go`；Header、URL、正文和摘要的实现位于同目录的 `redaction_*.go`。Core 在编码落库前应用统一策略；SDK 和插件不维护脱敏规则，也不发送可信脱敏标记。

| 部分 | 当前处理 |
|---|---|
| HTTP Header | 只保留 accept、content-type、openai-beta、originator、user-agent、x-openai-previous-response-id、retry-after、retry-after-ms；Authorization、Cookie、API Key、未知自定义头不保留 |
| 会话类 Header | session/conversation/turn-state 按统一别名、优先级和多值边界计算 xxh3-128；不保存原值。传入的 trace 指纹均不可信，入口和出站指纹都由原始头重新计算 |
| URL | 删除用户名密码、整个查询串和 fragment；无法安全解析的 URL 使用占位符 |
| 结构化凭证 | JSON、URL 编码表单、multipart 共用 access_token、refresh_token、id_token、api_key、client_secret、authorization、password、cookie 清单，值替换为 [REDACTED]；支持大小写与常见分隔符变体 |
| 图片/附件 | 删除图片块、data:image URI、图片/掩码字段，以及 b64_json、partial_image_b64、partial_image、image_b64；image_generation_call 的 result 按类型删除，普通工具 result 保留。multipart 文件/二进制部分不保存，直接丢弃读取，避免构造文件副本 |
| 错误摘要 | 对 reason、plugin_error、final.message 中匹配到的 Bearer、sk-*、凭证赋值、token/secret/session 标签、图片字段/data URI、邮箱、URL 敏感部分进行替换；普通监控摘要也复用相同实现 |
| 监控事件 detail | 凭证及 token/private_key/secret/session 等敏感键由 Core 的 traceredaction 统一识别，值替换为 [REDACTED]；深度、条目数与长度裁剪仍属于 Core 的事件格式限制 |

### 边界

- 这不是对所有用户内容的通用隐私擦除。普通 prompt、业务字段、正文中的邮箱、普通工具输出保留；不凭字符串熵猜测任意未知字段是不是密钥或 base64。
- `encrypted_content` 原文保留以支持续接/重放，Core 额外记录其 xxh3-128 指纹；指纹提取属于诊断元数据，不是解密或删除。
- 未脱敏正文保持字节不变；需要脱敏的 JSON 只解码一次，在本次独有的树上完成凭证和图片处理，再编码。不会修改实际发送或收到的原始缓冲区。
- 内部诊断协议只携带原始数据及采集长度，不携带脱敏状态。Core 不信任请求中自称已脱敏的字段；每份要存储的正文只在 Core 执行脱敏。已知采集不完整的正文直接省略。
- 对损坏 JSON 中的敏感字段、图片类型/内容提示或不可判定的转义键，以及无法安全解析的表单/multipart，省略正文并保留原因、原长度。无敏感提示的普通损坏 JSON 仍可留作诊断。
- 图片端点的 `application/x-www-form-urlencoded` 请求整体省略正文（包括 prompt），保留 `image_input` 原因与原长度；未知字段可能携带图片，不能因其他字段已脱敏就认定剩余内容安全。入口与出站使用同一规则，普通端点的表单仍按字段脱敏。
- 这些规则仅作用于 trace/监控副本，不改变模型请求、计费或主请求采集范围，也不重写历史 trace。

增加新的主模型请求模式时，使用 `buildForwardHTTPClient` 或主请求 WebSocket 入口并传递当前 context。辅助网络操作使用普通客户端，不加入 trace；无需引入错误分类、根因选择或新的 trace 数据结构。

