# 模型决策与执行

模型选择统一由 Core `internal/dispatchresolver` 决定。平台插件只声明 DSL，
HTTP、WebSocket 和 Host 转发共用解析器；协议适配器不得根据模型注册表再选模型。

## 决策优先级

1. 命中的分组 DSL 规则。
2. 命中的平台 DSL 规则。
3. 原样模型候选（identity）。未注册模型不会被改成默认模型。

匹配规则整体生效，分组规则不会隐式继承平台的回退目标。
`ClientModel` 用于规则匹配与审计，`SchedulingModel` 用于账号筛选，
`WireModel` 是请求实际发送的模型。操作门禁和超时策略随计划保留。

OpenAI 平台的规则在 `airgate-openai/backend/internal/gateway/model_policy.go`：
Claude 家族转换、候选备用模型、图像别名和上下文超限目标均在此声明。
图像别名在选账号前生效；图像协议适配器只把计划写入 JSON/multipart。
异步图像任务保存选定模型作为展示元数据，并单独保存 `client_model`。执行时用原始
client model 回到 Core 重新决策，不把 wire model 当作路由输入。缺少此字段的旧任务
明确失败，需重新创建；不做兼容推断。

## 上下文超限

规则字段 `context_window_fallback` 指定遇到上下文超限后的 client model，
允许使用与候选相同的模板变量；空值禁用该规则的回退。

```json
{
  "id": "text",
  "when": {"paths": ["/v1/responses"], "methods": ["POST", "WS"]},
  "operation": "chat.generate",
  "context_window_fallback": "gpt-5.6-sol",
  "candidates": [{"scheduling": "${model}", "wire": "${model}"}]
}
```

插件检测超限/命中既有超限缓存时只发送 `ModelFallbackReason=context_window`。
Core 使用请求路径、客户端模型和分组规则解析目标，再重新执行账号能力和操作权限检查。
插件不能通过响应携带任意目标。无配置、不合法原因、目标与当前 client model 相同、
流已提交或超过回退次数时不切换。HTTP 与 Host 的流式/非流式路径使用同一决策函数。
原有超限缓存触发时机不变，不新增静默重试。

## 定价

模型注册表只提供模型元数据和价格。未注册模型可以用系列/默认价格计算费用，
但查价不会修改 ClientModel、SchedulingModel、WireModel 或请求正文。

## 契约更新

SDK `DispatchRule` 及其 protobuf 声明 `context_window_fallback`，用于插件向 Core 注册策略。
`DispatchPlan` 和其 protobuf 不携带回退策略。插件的超限缓存按请求和实际 wire model 隔离，
只报告超限事实；是否启用回退、回退目标和循环防护由 Core 决定。
删除 `ForwardOutcome.RerouteClientModel` 及其旧 getter，改用回退原因。
protobuf 旧字段号 11 和字段名保留为 reserved，不实现旧字段读取或转换。
go-plugin 握手升至 v2，阻止旧插件与新 Core 混用。Core 与所有插件必须使用同一
新版 SDK 重新构建后一起加载；运行时资源管理协议版本不变。
