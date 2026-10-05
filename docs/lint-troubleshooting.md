# Go lint 包加载失败

`no go files to analyze` 不一定表示缺少 Go 源文件或需要 `go mod tidy`。
如果 `go list ./...` 正常、golangci-lint 却加载到 0 个包，先检查其子进程
是否能读写 Go 构建缓存。受限环境中的 `Access is denied` 可能被包加载器
掩盖为这条错误。全局临时目录不可写还可能被误报为已有 lint 进程运行。

`make lint` 和 `make lint-unused` 使用仓库 `.tools/` 内的 Go 构建缓存、
golangci-lint 缓存和临时目录；安装固定版本工具时也使用相同环境。
这些目录已被 Git 忽略，路径根据当前检出目录计算，不依赖开发者机器。
原有全局缓存和权限不会被修改。

没有 Make 的 PowerShell 环境，可从仓库根目录运行已安装的固定版本工具：

```powershell
$lintRepo = (Get-Location).Path
$lintEnvironment = @{
    GOCACHE = Join-Path $lintRepo '.tools/go-build-cache'
    GOLANGCI_LINT_CACHE = Join-Path $lintRepo '.tools/golangci-cache'
    TMPDIR = Join-Path $lintRepo '.tools/lint-temp'
    TMP = Join-Path $lintRepo '.tools/lint-temp'
    TEMP = Join-Path $lintRepo '.tools/lint-temp'
}
$previousEnvironment = @{}
try {
    foreach ($name in $lintEnvironment.Keys) {
        $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        New-Item -ItemType Directory -Force $lintEnvironment[$name] | Out-Null
        [Environment]::SetEnvironmentVariable($name, $lintEnvironment[$name], 'Process')
    }
    Push-Location (Join-Path $lintRepo 'backend')
    try {
        & (Join-Path $lintRepo '.tools/bin/golangci-lint.exe') run ./...
        if ($LASTEXITCODE -ne 0) { throw 'Go lint failed' }
    } finally {
        Pop-Location
    }
} finally {
    foreach ($name in $previousEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], 'Process')
    }
}
```

不要通过忽略退出码、禁用分析器或修改依赖来绕过包加载失败。
环境修复后必须重新执行完整 lint；格式化检查通过不代表静态分析通过。
