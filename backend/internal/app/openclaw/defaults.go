// Package openclaw 提供 OpenClaw 一键接入相关的配置与资源。
//
// 本包只依赖 settings 域，不引入 HTTP 层。所有默认值集中在这里，
// 任何 setting key 未配置时走默认值回退，这样：
//   - 管理员面板看到的是"空即默认"的体验
//   - /openclaw/* 路由无需访问数据库也能给出可用的脚本与文档
package openclaw

import _ "embed"

// Setting key 常量。统一加 "openclaw." 前缀，便于在 Setting 表中按前缀筛选。
const (
	GroupName = "openclaw"

	KeyEnabled             = "openclaw.enabled"
	KeyProviderName        = "openclaw.provider_name"
	KeyBaseURL             = "openclaw.base_url"
	KeyModelsPreset        = "openclaw.models_preset"
	KeyInstallDoc          = "openclaw.install_doc"
	KeyMemorySearchEnabled = "openclaw.memory_search_enabled"
	KeyMemorySearchModel   = "openclaw.memory_search_model"
)

// DefaultProviderName 是写入 openclaw.json 的默认 provider 键名。
const DefaultProviderName = "airgate"

// DefaultMemorySearchModel 是 memorySearch 用的默认 embedding 模型。
const DefaultMemorySearchModel = "text-embedding-3-small"

// DefaultModelsPresetJSON 是管理员未配置时展示给用户挑选的模型预设。
//
// 字段与 openclaw.json 里 provider.models[] 的形状一致（id/api/reasoning/input），
// 额外带一个 label 供脚本展示给用户。
const DefaultModelsPresetJSON = `[
  {
    "id": "gpt-5.4",
    "label": "GPT-5.4 (推荐)",
    "api": "openai-responses",
    "reasoning": true,
    "input": ["text", "image"]
  },
  {
    "id": "claude-sonnet-4-6",
    "label": "Claude Sonnet 4.6",
    "api": "anthropic-messages",
    "reasoning": true,
    "input": ["text", "image"]
  },
  {
    "id": "claude-opus-4-6",
    "label": "Claude Opus 4.6",
    "api": "anthropic-messages",
    "reasoning": true,
    "input": ["text", "image"]
  }
]`

// DefaultInstallDoc 是管理员未配置时 /openclaw/doc 返回的 markdown。
//
// 支持的占位符（与 SiteName / BaseURL / InstallCommand 对应）：
//   - {{site_name}}
//   - {{base_url}}
//   - {{install_command}}
const DefaultInstallDoc = `# 使用 {{site_name}} 一键接入 openclaw

[openclaw](https://github.com/openclaw/openclaw) 是一款可以运行在本机的个人 AI 助理。
{{site_name}} 已经兼容 openclaw 所需的 OpenAI / Anthropic 协议，你只需要运行一行命令即可完成接入：

## 一键安装

复制下面这行命令到终端执行：

~~~bash
{{install_command}}
~~~

脚本会：

1. 交互式提示你粘贴一把 {{site_name}} 的 API Key（从个人中心 → API 密钥创建）
2. 拉取管理员预设的可选模型列表让你勾选
3. 自动生成 ~/.openclaw/openclaw.json
4. 如果已有旧配置，会备份为 openclaw.json.bak.<时间戳>

完成后按 openclaw 官方文档启动即可：

~~~bash
openclaw gateway
~~~

## 手动配置

如果你不想运行脚本，也可以按照下面的结构手动创建 ~/.openclaw/openclaw.json：

~~~json
{
  "models": {
    "mode": "merge",
    "providers": {
      "airgate": {
        "baseUrl": "{{base_url}}/v1",
        "apiKey": "sk-你的key",
        "models": [
          { "id": "gpt-5.4", "name": "gpt-5.4", "api": "openai-responses", "reasoning": true, "input": ["text", "image"] }
        ]
      }
    }
  },
  "agents": {
    "defaults": {
      "model": { "primary": "airgate/gpt-5.4" }
    }
  }
}
~~~

## 常见问题

**Q: 脚本提示 API Key 无效怎么办？**
A: 确认 Key 没有粘贴多余的空格/换行；确认 Key 未过期、未停用，且额度未用尽。

**Q: 想换模型或重新配置？**
A: 重新运行一次一键命令即可；旧配置会被自动备份。

**Q: 如何卸载？**
A: 删除 ~/.openclaw/openclaw.json 即可，脚本不会在系统其它位置写文件。
`

// installScriptTemplate 是 /openclaw/install.sh 返回的脚本模板，由 go:embed 打进二进制。
//
//go:embed assets/install.sh.tmpl
var installScriptTemplate string

// InstallScriptTemplate 返回安装脚本模板原文。
func InstallScriptTemplate() string {
	return installScriptTemplate
}
