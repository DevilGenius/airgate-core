# HTTP 资源边界

HTTP 服务默认最多接纳 1024 个在途 handler，请求体在读/已读数据合计预算 128 MiB。读取完成后仍保留预算，直到整个请求结束，覆盖排队和上游转发期间保留的 body。chunked 请求按实际读取计入，不依赖 Content-Length。JSON 对象、RPC 副本及其他缓存并不包含在这个原始请求体预算中。

可在现有 config.yaml 的 server 下调整，无需 SQL 或迁移：

```yaml
server:
  max_in_flight_requests: 1024
  max_buffered_body_bytes: 134217728
```

不填写或非正值使用默认值。容量满返回 503 和 Retry-After: 1；用户/API Key 自身的并发、RPM 超限仍返回 429。GET /healthz 不占用业务准入名额。已认证网关请求在读取 body 前检查用户/API Key 限制。

读 header 最长 10 秒，读取整个请求最长 60 秒，keep-alive 空闲连接最长 90 秒；读取完成后的合法长流不使用固定总写超时。网关单请求 10/32 MiB 上限继续生效。上述实例容量应结合实际机器内存、请求体大小和上游并发调整。

单次下游写入/刷新最多等待 30 秒，大响应按 32 KiB 分块更新写截止。等待下一条上游事件期间清除写截止，避免 HTTP/2 的长生成被误终止。写入或刷新失败会取消请求上下文，SDK 流调用随之退出；普通响应、SSE 和 Gin 包装链使用同一原生 HTTP Writer 保护。
