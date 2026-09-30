# 仪表盘用量估算

仅汇总未禁用、未删除的 OpenAI OAuth 非 Free 账号；套餐过期后按有效套餐判断，套餐未知的账号不纳入。

仪表盘展示格式为 `$短期可用总量 $5h余量合计 估算可用时间`：总量和可用时间使用与其他卡片一致的较大字号，5h 小计无括号、使用较小字号。watcher TUI 使用两行内容，与并排的流量、调度卡片高度一致：上方为 `$短期可用总量 / $5h余量合计`，下方为估算可用时间。

- 短期可用总量 = 无 5h 窗口账号的 7d 估算余量 + 有 5h 窗口账号的 5h 估算余量。
- 5h 小计仅是有 5h 窗口账号的 5h 余量合计，已经包含在短期总量中。
- 逐账号依据实际基础窗口判断，不根据 Plus、Team、ProLite、Pro 等套餐推断；同为 Team 的账号可分别使用 5h 和 7d。
- 可用时间 = 短期总量 / 最近一分钟账号标准成本消耗速率。标题继续显示 1min/10min 速率。
- 同套餐、同窗口共享校准值；5h 与 7d 不混用校准。缺少校准或观测已过期的账号不贡献余量，已知部分仍可形成保守估算。
- 额度未知与额度耗尽分别表示为缺失值和零。已知没有 5h 账号时，5h 小计为 `$0`。
- 图标变色和 watcher 提醒均依据短期可用总量，沿用原有阈值、恢复线和冷却配置；5h 小计仅作展示。

## API

仪表盘统计的 `usage_estimates` 套餐数组改为 `usage_estimate` 对象：

```json
{
  "usage_estimate": {
    "total": {"status": "ready", "account_count": 5, "remaining_cost": 3150, "remaining_minutes": 315},
    "five_hour": {"status": "ready", "account_count": 2, "remaining_cost": 150, "remaining_minutes": 15}
  }
}
```

凭证账号 overview API 的 `usage_estimate` 保留 `standard_cost_per_minute_1m`、`standard_cost_per_minute_10m`，并将 `plus_5h/pro_5h/plus_7d/pro_7d` 替换为 `total` 与 `five_hour`。两个子对象使用 `status`、`account_count`、`available_standard_cost`、`available_minutes`。watcher TUI 使用此接口。

`account_count` 是选入该范围的账号数，不代表每个账号均有有效校准。已知正余额但当前消耗为零时，分钟数为空，界面沿用 `>1000h`；已耗尽时为 `0m`。这次 API 字段变更需要前后端和 watcher 同步更新。
