# Trace 性能与内存测试（历史基准）

> 本文数值采集于脱敏迁移到 Core 之前，保留用于历史对照，不代表当前 SDK 热路径或当前端到端性能。当前 SDK 只采集原始诊断；脱敏统一在 Core 落库前进行，旧的可信脱敏元数据优化已移除。下方命令已更新为当前包位置。

## 方法与范围

- 平台：Windows amd64，Intel Core i5-13600KF，go1.26.5。
- 单核基准（GOMAXPROCS=1、-cpu=1），每项重复 3 次，取中位数；SDK/Core 每轮 200ms，BAS 插件本地 TLS 转发每轮 300ms。
- 使用生成的 4 KiB、64 KiB、1 MiB、8 MiB 文本及中文/转义工具参数样本；不是用户真实请求。
- 基准串行执行，未同时运行另一组 CPU 基准。开发机其他进程仍可能产生噪声，小于约 1% 的差异不宜解读为显著变化。
- SDK 基准隔离采集器 CPU/分配成本，输出写入 Discard；不能用其 MB/s 推导真实网络吞吐。BAS 基准执行实际插件 Forward + localhost TLS + BPS 转换，未访问真实上游，不包含 Core/gRPC/数据库和公网延迟。
- B/op 是累计分配量，不是存活堆或进程工作集。未做生产并发容量或 p95/p99 评估。

## 保留期与释放语义

当前数据库 trace 不设过期时间、不自动清理，仅支持管理员手动清空。普通监控事件仍按独立保留策略清理。本报告性能数据来自保留策略调整前的采集器测试；此次调整不改变被测请求载荷。

成功请求结束时主动清除 SDK 采集器和 Gin 池化 context 的大对象引用；由 Go GC 随后回收，不保证工作集立即归还操作系统。失败请求的原文/诊断仍由异步队列持有，直到编码落库或丢弃完成。队列容量 32，原始正文预算 512 MiB，满载非阻塞降级为只留事件。该预算不含并发请求、序列化临时内存和可复用压缩器工作区。

在完成一个 8 MiB 请求后，故意继续持有 context 和消费完毕的 response，再强制 GC：

| 指标 | 优化前 | 优化后 |
|---|---:|---:|
| 相对基线新增可达堆 | 8405720 bytes | 8832 bytes |
| 采集器保留请求数 | 1 | 0 |

该测试隔离 trace 自身引用，不代表整个 HTTP 栈的内存；断言同时验证正文、响应和 SSE 工作缓冲区已清空，迟到的事件不能重新填入，失败快照依然完整。

## 关闭开关的增量成本

| 请求 | 无包装基线 ns/op | trace 关闭 ns/op | 增量 ns/op | 增量 B/op |
|---|---:|---:|---:|---:|
| 4 KiB | 558.7 | 599.4 | 40.7 | 0 |
| 64 KiB | 595.2 | 605.9 | 10.7 | 0 |
| 1024 KiB | 576.0 | 600.7 | 24.7 | 0 |
| 8192 KiB | 475.7 | 482.9 | 7.2 | 0 |

关闭时没有 trace 额外分配，但仍有数纳秒到数十纳秒的分支/context 查询成本，不能宣称完全零开销。

## 开启后的插件采集器成本

| SDK 测试 | 优化前 ms/op | 优化后 ms/op | 时间下降 | 分配前 KiB/op | 分配后 KiB/op |
|---|---:|---:|---:|---:|---:|
| HTTP/64KiB/on_success | 0.0167 | 0.0071 | 57.3% | 217.7 | 74.8 |
| HTTP/1024KiB/on_success | 0.2385 | 0.0764 | 68.0% | 3218.7 | 1034.8 |
| HTTP/8192KiB/on_success | 1.9791 | 0.7490 | 62.2% | 24876.0 | 8202.9 |
| HTTP/64KiB/on_failure | 0.6564 | 0.2887 | 56.0% | 861.7 | 397.7 |
| HTTP/1024KiB/on_failure | 10.2813 | 4.3797 | 57.4% | 13470.9 | 6158.1 |
| HTTP/8192KiB/on_failure | 79.8432 | 34.7095 | 56.5% | 106807.0 | 49167.1 |
| SSE/1000Events/on=true | 0.0789 | 0.0425 | 46.1% | 280.4 | 75.0 |
| RepresentativeFailure/1024KiB | 12.6029 | 6.7174 | 46.7% | 9646.5 | 5749.0 |

1000 个 SSE 事件的分配次数从 1053 降至 36。请求仍完整复制一次，最近事件缓冲区复用，凭证与图片共用一次 JSON 解析；不通过采样或截断正常正文换取性能。

## BAS 插件实际 Forward（本地模拟上游）

| 请求 | trace 关闭 ms/op | trace 开启 ms/op | 差值 ms | 开启/关闭差值 |
|---|---:|---:|---:|---:|
| 64 KiB 成功 | 2.390 | 2.485 | 0.095 | 4.0% |
| 64 KiB 失败 | 2.210 | 2.555 | 0.344 | 15.6% |
| 1024 KiB 成功 | 27.979 | 28.132 | 0.153 | 0.5% |
| 1024 KiB 失败 | 27.361 | 31.552 | 4.190 | 15.3% |

失败路径要同步生成脱敏诊断，并非全部工作都在后台。Core 原文复制、gRPC 序列化和数据库还会增加成本，不能把本表当成系统整体延迟。

## Core 原文快照、入队及后台编码

| 工作 | 请求大小 | ms/op | KiB/op | 执行位置 |
|---|---:|---:|---:|---|
| 入口原文复制 | 64 KiB | 0.0125 | 64.4 | 请求路径 |
| 入口原文复制 | 1024 KiB | 0.1750 | 1024.4 | 请求路径 |
| 入口原文复制 | 8192 KiB | 0.6461 | 8192.4 | 请求路径 |
| 入队+取出（不编码） | 64 KiB | 0.0032 | 1.3 | 请求路径 |
| 入队+取出（不编码） | 1024 KiB | 0.0032 | 1.3 | 请求路径 |
| JSON/脱敏/hash/gzip | 4 KiB × 2 | 0.108 | 98.2 | 后台 worker |
| JSON/脱敏/hash/gzip | 64 KiB × 2 | 1.454 | 1562.7 | 后台 worker |
| JSON/脱敏/hash/gzip | 1024 KiB × 2 | 21.783 | 30971.1 | 后台 worker |
| JSON/脱敏/hash/gzip | 8192 KiB × 2 | 162.277 | 239067.1 | 后台 worker |

1 MiB 客户端正文加 1 MiB 上游正文的后台编码由 33.36 ms 降到 21.78 ms。大正文失败连续发生时，单 worker 和数据库仍可能成为瓶颈；512 MiB 队列预算不代表低内存模式。

## 改动与验证

- Finish 主动释放 SDK/Gin 引用；被淘汰的重试记录也清理；结束后拒绝迟到数据。
- 已知长度的 HTTP GetBody reader 使用一次预分配并转移所有权，避免增长缓冲区及重复复制；长度不一致仍有保护。
- SSE 最近事件复用容量，结束后清空，保留完整最后事件。
- 图片和凭证脱敏只解析一次 JSON，保留数字精度、Unicode/转义键行为。
- gzip writer 工作区复用，每次归还前 Reset 到 io.Discard，不保留输出 payload 引用。
- 回归覆盖请求逐字节保存、失败诊断有效性、完成后的 buffer 清空、迟到/并发读取、队列预算释放和 trace 不受事件保留期影响。

## 复现

在各仓库对应目录执行（Windows 已使用默认 PowerShell，无需再包一层 shell）。Go 缓存位置可按本机环境配置：

```powershell
$env:GOMAXPROCS="1"
# airgate-sdk
go test ./sdkgo/requesttrace -run '^$' -bench BenchmarkTrace -benchmem -benchtime=200ms -count=3 -cpu=1
go test ./sdkgo/requesttrace -run '^TestTraceRetainedHeapProfile$' -count=1 -v
# airgate-core/backend
go test ./internal/app/monitor ./internal/plugin -run '^$' -bench 'Benchmark(RequestTraceEncode|TraceIngressSnapshot|RequestTraceEnqueue)' -benchmem -benchtime=200ms -count=3 -cpu=1
# airgate-openai/backend
go test ./internal/gateway -run '^$' -bench BenchmarkBasispointsTraceForward -benchmem -benchtime=300ms -count=3 -cpu=1
```

本机原始输出保存在 airgate-sdk/.tools/trace-perf/ 的 baseline-*.txt 与 optimized-*.txt。测试没有请求正在运行的开发服务，也没有调用真实 BAS/OAuth 或产生计费。

## 最终回归状态

SDK requesttrace/gRPC、Core monitor/plugin、OpenAI basispoints/gateway/imgen/cmd-chat 包测试全部通过；请求结束与响应读取并发的回归额外重复 50 次通过。

尝试了 Go race detector，但本机 CGO_ENABLED=0，PATH 中未找到 gcc/clang，未运行竞态检测器，也没有安装工具链或修改全局环境。普通并发回归不等同于 race detector 通过。

## 统一脱敏后的局部基准

对本次重构前后分别运行 `BenchmarkSanitizeBody`：相同开发机、GOMAXPROCS=1、每项 200ms、重复 3 次，取中位数。输入含 64 个嵌套消息对象；只测 SDK 正文脱敏，不代表整条请求或数据库性能。

| 场景 | 重构前 µs/op | 重构后 µs/op | 耗时下降 | 分配次数 前→后 |
|---|---:|---:|---:|---:|
| 普通正文 64 KiB | 306.250 | 267.081 | 12.8% | 676 → 545 |
| 普通正文 1 MiB | 4400.994 | 3819.183 | 13.2% | 680 → 549 |
| 含凭证 64 KiB | 375.851 | 333.298 | 11.3% | 1008 → 876 |
| 含图片字段 64 KiB | 385.318 | 332.270 | 13.8% | 1005 → 874 |

优化来自凭证/图片一次遍历、原地处理本次解码树、避免重建未变化的 map/slice，以及只检查 data:image 前缀而非将整段文本转小写。普通正文仍保持原字节，大整数保留精度，带可信脱敏元数据的正文不重复解析。

原始输出：`airgate-sdk/.tools/trace-redaction/before.txt`、`after.txt`。复现命令：

```powershell
$env:GOMAXPROCS="1"
# airgate-core/backend
go test ./internal/app/monitor/traceredaction -run '^$' -bench '^BenchmarkSanitizeBody$' -benchmem -benchtime=200ms -count=3 -cpu=1
```

