param(
  [ValidateSet("start", "stop", "restart", "status", "build")]
  [string]$Action = "start",

  [switch]$Build,
  [switch]$Install,
  [switch]$ForcePortStop
)

$ErrorActionPreference = "Stop"

if ($PSVersionTable.PSVersion.Major -lt 7) {
  throw "PowerShell 7+ is required. Current: $($PSVersionTable.PSVersion)"
}

$CoreRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$WorkspaceRoot = Resolve-Path (Join-Path $CoreRoot "..")
$SdkTheme = Join-Path $WorkspaceRoot "airgate-sdk\theme"
$OpenAIPluginRoot = Join-Path $WorkspaceRoot "airgate-openai"
$ClaudePluginRoot = Join-Path $WorkspaceRoot "airgate-claude"
$KiroPluginRoot = Join-Path $WorkspaceRoot "airgate-kiro"
$PlaygroundPluginRoot = Join-Path $WorkspaceRoot "airgate-playground"
$EpayPluginRoot = Join-Path $WorkspaceRoot "airgate-epay"
$HealthPluginRoot = Join-Path $WorkspaceRoot "airgate-health"
$StudioPluginRoot = Join-Path $WorkspaceRoot "airgate-studio"
$WebDir = Join-Path $CoreRoot "web"
$BackendDir = Join-Path $CoreRoot "backend"
$WebDist = Join-Path $WebDir "dist"
$WebDistEmbed = Join-Path $BackendDir "internal\web\webdist"
$StateDir = Join-Path $CoreRoot ".dev"
$DevConfigFile = Join-Path $StateDir "config.yaml"
$BackendPidFile = Join-Path $StateDir "backend.pid"
$FrontendPidFile = Join-Path $StateDir "frontend.pid"
$BackendOut = Join-Path $BackendDir "tmp\backend.out.log"
$BackendErr = Join-Path $BackendDir "tmp\backend.err.log"
$BackendDevExe = Join-Path $BackendDir "tmp\airgate-core-dev.exe"
$FrontendOut = Join-Path $WebDir "tmp\frontend.out.log"
$FrontendErr = Join-Path $WebDir "tmp\frontend.err.log"
$FrontendDevPort = 80
$LocalUnusedLintRan = $false

$DeclaredPluginSpecs = @(
  [pscustomobject]@{
    Name = "gateway-openai"
    Root = $OpenAIPluginRoot
    WebDir = Join-Path $OpenAIPluginRoot "web"
    BackendDir = Join-Path $OpenAIPluginRoot "backend"
    WebDist = Join-Path $OpenAIPluginRoot "web\dist"
    EmbedDir = Join-Path $OpenAIPluginRoot "backend\internal\gateway\webdist"
    WatchPidFile = Join-Path $StateDir "gateway-openai-web.pid"
    WatchOut = Join-Path $OpenAIPluginRoot "tmp\web-watch.out.log"
    WatchErr = Join-Path $OpenAIPluginRoot "tmp\web-watch.err.log"
  },
  [pscustomobject]@{
    Name = "gateway-claude"
    Root = $ClaudePluginRoot
    WebDir = Join-Path $ClaudePluginRoot "web"
    BackendDir = Join-Path $ClaudePluginRoot "backend"
    WebDist = Join-Path $ClaudePluginRoot "web\dist"
    EmbedDir = Join-Path $ClaudePluginRoot "backend\internal\gateway\webdist"
    WatchPidFile = Join-Path $StateDir "gateway-claude-web.pid"
    WatchOut = Join-Path $ClaudePluginRoot "tmp\web-watch.out.log"
    WatchErr = Join-Path $ClaudePluginRoot "tmp\web-watch.err.log"
  },
  [pscustomobject]@{
    Name = "gateway-kiro"
    Root = $KiroPluginRoot
    WebDir = Join-Path $KiroPluginRoot "web"
    BackendDir = Join-Path $KiroPluginRoot "backend"
    WebDist = Join-Path $KiroPluginRoot "web\dist"
    EmbedDir = Join-Path $KiroPluginRoot "backend\internal\gateway\webdist"
    WatchPidFile = Join-Path $StateDir "gateway-kiro-web.pid"
    WatchOut = Join-Path $KiroPluginRoot "tmp\web-watch.out.log"
    WatchErr = Join-Path $KiroPluginRoot "tmp\web-watch.err.log"
  },
  [pscustomobject]@{
    Name = "airgate-playground"
    Root = $PlaygroundPluginRoot
    WebDir = Join-Path $PlaygroundPluginRoot "web"
    BackendDir = Join-Path $PlaygroundPluginRoot "backend"
    WebDist = Join-Path $PlaygroundPluginRoot "web\dist"
    EmbedDir = Join-Path $PlaygroundPluginRoot "backend\internal\playground\webdist"
    WatchPidFile = Join-Path $StateDir "airgate-playground-web.pid"
    WatchOut = Join-Path $PlaygroundPluginRoot "tmp\web-watch.out.log"
    WatchErr = Join-Path $PlaygroundPluginRoot "tmp\web-watch.err.log"
  },
  [pscustomobject]@{
    Name = "payment-epay"
    Root = $EpayPluginRoot
    WebDir = Join-Path $EpayPluginRoot "web"
    BackendDir = Join-Path $EpayPluginRoot "backend"
    WebDist = Join-Path $EpayPluginRoot "web\dist"
    EmbedDir = Join-Path $EpayPluginRoot "backend\internal\payment\webdist"
    WatchPidFile = Join-Path $StateDir "payment-epay-web.pid"
    WatchOut = Join-Path $EpayPluginRoot "tmp\web-watch.out.log"
    WatchErr = Join-Path $EpayPluginRoot "tmp\web-watch.err.log"
  },
  [pscustomobject]@{
    Name = "airgate-health"
    Root = $HealthPluginRoot
    WebDir = Join-Path $HealthPluginRoot "web"
    BackendDir = Join-Path $HealthPluginRoot "backend"
    WebDist = Join-Path $HealthPluginRoot "web\dist"
    EmbedDir = Join-Path $HealthPluginRoot "backend\internal\health\webdist"
    WatchPidFile = Join-Path $StateDir "airgate-health-web.pid"
    WatchOut = Join-Path $HealthPluginRoot "tmp\web-watch.out.log"
    WatchErr = Join-Path $HealthPluginRoot "tmp\web-watch.err.log"
  },
  [pscustomobject]@{
    Name = "airgate-studio"
    Root = $StudioPluginRoot
    WebDir = Join-Path $StudioPluginRoot "web"
    BackendDir = Join-Path $StudioPluginRoot "backend"
    WebDist = Join-Path $StudioPluginRoot "web\dist"
    EmbedDir = Join-Path $StudioPluginRoot "backend\internal\studio\webdist"
    WatchPidFile = Join-Path $StateDir "airgate-studio-web.pid"
    WatchOut = Join-Path $StudioPluginRoot "tmp\web-watch.out.log"
    WatchErr = Join-Path $StudioPluginRoot "tmp\web-watch.err.log"
  }
)
$PluginSpecs = @()

function Write-Step([string]$Message) {
  Write-Host "==> $Message"
}

function Get-AvailablePluginSpecs {
  $available = [System.Collections.Generic.List[object]]::new()

  foreach ($plugin in $DeclaredPluginSpecs) {
    $missing = @()
    if (-not (Test-Path $plugin.WebDir)) {
      $missing += "web: $($plugin.WebDir)"
    }
    if (-not (Test-Path $plugin.BackendDir)) {
      $missing += "backend: $($plugin.BackendDir)"
    }

    if ($missing.Count -gt 0) {
      Write-Step "skipping $($plugin.Name); missing $($missing -join '; ')"
      continue
    }

    $available.Add($plugin)
  }

  $available.ToArray()
}

function Invoke-InDir([string]$Directory, [string]$Command) {
  Write-Step "$Directory > $Command"
  Push-Location $Directory
  try {
    pwsh -NoLogo -NoProfile -Command $Command
    if ($LASTEXITCODE -ne 0) {
      throw "Command failed with exit code ${LASTEXITCODE}: $Command"
    }
  } finally {
    Pop-Location
  }
}

function Get-GoEnvCommand([string]$Command) {
  "`$env:GOTOOLCHAIN = 'local'; `$env:GOPRIVATE = 'github.com/DevilGenius/airgate-sdk'; `$env:GONOPROXY = 'github.com/DevilGenius/airgate-sdk'; `$env:GONOSUMDB = 'github.com/DevilGenius/airgate-sdk'; $Command"
}

function Set-GoEnvVars {
  $env:GOTOOLCHAIN = "local"
  $env:GOPRIVATE = "github.com/DevilGenius/airgate-sdk"
  $env:GONOPROXY = "github.com/DevilGenius/airgate-sdk"
  $env:GONOSUMDB = "github.com/DevilGenius/airgate-sdk"
}

function Invoke-CoreUnusedLint {
  if ($script:LocalUnusedLintRan) {
    return
  }

  Write-Step "checking Go unused/staticcheck"
  Assert-Command "go"
  Push-Location $CoreRoot
  try {
    Set-GoEnvVars
    & go run ./backend/cmd/local-lint -unused
    if ($LASTEXITCODE -ne 0) {
      throw "Command failed with exit code ${LASTEXITCODE}: go run ./backend/cmd/local-lint -unused"
    }
    $script:LocalUnusedLintRan = $true
  } finally {
    Pop-Location
  }
}

function Assert-Command([string]$Name) {
  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "Missing command: $Name"
  }
}

function Invoke-PnpmInstall([string]$Directory, [switch]$Force) {
  $command = if ($Force) { "pnpm install --force" } else { "pnpm install" }
  try {
    Invoke-InDir $Directory $command
  } catch {
    Write-Step "pnpm install failed; approving esbuild build script and retrying"
    Invoke-InDir $Directory "pnpm approve-builds esbuild"
    Invoke-InDir $Directory $command
  }
}

function Assert-Paths {
  if (-not (Test-Path $SdkTheme)) {
    throw "SDK theme not found: $SdkTheme"
  }
  if (-not (Test-Path $WebDir)) {
    throw "Core web not found: $WebDir"
  }
  if (-not (Test-Path $BackendDir)) {
    throw "Core backend not found: $BackendDir"
  }
  foreach ($plugin in $PluginSpecs) {
    if (-not (Test-Path $plugin.WebDir)) {
      throw "$($plugin.Name) web not found: $($plugin.WebDir)"
    }
    if (-not (Test-Path $plugin.BackendDir)) {
      throw "$($plugin.Name) backend not found: $($plugin.BackendDir)"
    }
  }
}

function Ensure-Dirs {
  New-Item -ItemType Directory -Force -Path `
    $StateDir, `
    (Join-Path $BackendDir "tmp"), `
    (Join-Path $WebDir "tmp"), `
    $WebDistEmbed | Out-Null

  foreach ($plugin in $PluginSpecs) {
    New-Item -ItemType Directory -Force -Path `
      (Join-Path $plugin.Root "tmp"), `
      $plugin.EmbedDir | Out-Null
  }
}

function Install-Deps {
  Assert-Command "pnpm"
  Assert-Command "go"
  Invoke-PnpmInstall $SdkTheme
  Invoke-InDir $SdkTheme "pnpm build"
  Invoke-PnpmInstall $WebDir -Force
  Invoke-InDir $BackendDir (Get-GoEnvCommand "go mod download")
  foreach ($plugin in $PluginSpecs) {
    Ensure-PluginGoWork $plugin
    Invoke-PnpmInstall $plugin.WebDir -Force
    Invoke-InDir $plugin.BackendDir (Get-GoEnvCommand "go mod download")
  }
}

function Sync-Webdist {
  if (-not (Test-Path (Join-Path $WebDist "index.html"))) {
    throw "Core web dist is missing. Run: .\deploy\dev.ps1 build"
  }

  $resolvedCore = (Resolve-Path $CoreRoot).Path
  $resolvedEmbed = (Resolve-Path $WebDistEmbed).Path
  if (-not $resolvedEmbed.StartsWith($resolvedCore, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to sync outside core repo: $resolvedEmbed"
  }

  Get-ChildItem -LiteralPath $WebDistEmbed -Force |
    Where-Object { $_.Name -ne ".gitkeep" } |
    Remove-Item -Recurse -Force

  Copy-Item -Path (Join-Path $WebDist "*") -Destination $WebDistEmbed -Recurse -Force
  Write-Step "synced web/dist -> backend/internal/web/webdist"
}

function Ensure-PluginGoWork($Plugin) {
  $goWork = Join-Path $Plugin.BackendDir "go.work"
  $desired = @"
go 1.26.3

use .

replace github.com/DevilGenius/airgate-sdk => ../../airgate-sdk
"@

  if (-not (Test-Path $goWork) -or ((Get-Content -Raw $goWork) -ne $desired)) {
    Set-Content -Path $goWork -Value $desired
    Write-Step "wrote $($Plugin.Name) backend/go.work for local SDK"
  }
}

function ConvertTo-YamlSingleQuoted([string]$Value) {
  "'" + $Value.Replace("'", "''") + "'"
}

function Get-DevPluginYamlLines {
  $lines = [System.Collections.Generic.List[string]]::new()
  $lines.Add("  dev:")
  foreach ($plugin in $PluginSpecs) {
    $path = (Resolve-Path $plugin.BackendDir).Path.Replace("\", "/")
    $lines.Add("    - name: $($plugin.Name)")
    $lines.Add("      path: $(ConvertTo-YamlSingleQuoted $path)")
  }
  $lines.ToArray()
}

function Write-DevConfig {
  $sourceConfig = Join-Path $BackendDir "config.yaml"
  if (-not (Test-Path $sourceConfig)) {
    throw "Backend config not found: $sourceConfig"
  }

  $devLines = [string[]](Get-DevPluginYamlLines)
  $sourceLines = Get-Content -Path $sourceConfig
  $out = [System.Collections.Generic.List[string]]::new()
  $sawPlugins = $false
  $inPlugins = $false
  $insertedDev = $false
  $skippingDev = $false

  foreach ($line in $sourceLines) {
    if ($inPlugins) {
      if ($line -match "^\S" -and $line -notmatch "^plugins:\s*$") {
        if (-not $insertedDev) {
          $out.AddRange($devLines)
          $insertedDev = $true
        }
        $inPlugins = $false
        $skippingDev = $false
        $out.Add($line)
        continue
      }

      if ($skippingDev) {
        if ($line -match "^\s{2}[A-Za-z0-9_-]+:\s*") {
          $skippingDev = $false
        } else {
          continue
        }
      }

      if ($line -match "^\s{2}dev:\s*(\[\])?\s*$") {
        $out.AddRange($devLines)
        $insertedDev = $true
        $skippingDev = $true
        continue
      }
    }

    if ($line -match "^plugins:\s*$") {
      $sawPlugins = $true
      $inPlugins = $true
      $insertedDev = $false
      $skippingDev = $false
      $out.Add($line)
      continue
    }

    $out.Add($line)
  }

  if ($inPlugins -and -not $insertedDev) {
    $out.AddRange($devLines)
  }

  if (-not $sawPlugins) {
    if ($out.Count -gt 0 -and $out[$out.Count - 1].Trim() -ne "") {
      $out.Add("")
    }
    $out.Add("plugins:")
    $out.AddRange($devLines)
  }

  Set-Content -Path $DevConfigFile -Value $out
  Write-Step "wrote dev config with $($PluginSpecs.Count) plugins: $DevConfigFile"
  $DevConfigFile
}

function Sync-PluginWebdist($Plugin) {
  if (-not (Test-Path (Join-Path $Plugin.WebDist "index.js"))) {
    throw "$($Plugin.Name) web dist is missing. Run: .\deploy\dev.ps1 build"
  }

  $resolvedRoot = (Resolve-Path $Plugin.Root).Path
  $resolvedEmbed = (Resolve-Path $Plugin.EmbedDir).Path
  if (-not $resolvedEmbed.StartsWith($resolvedRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to sync outside plugin repo: $resolvedEmbed"
  }

  Get-ChildItem -LiteralPath $Plugin.EmbedDir -Force |
    Where-Object { $_.Name -ne ".gitkeep" } |
    Remove-Item -Recurse -Force

  Copy-Item -Path (Join-Path $Plugin.WebDist "*") -Destination $Plugin.EmbedDir -Recurse -Force
  Write-Step "synced $($Plugin.Name) web/dist -> $($Plugin.EmbedDir)"
}

function Build-Plugin($Plugin) {
  Ensure-PluginGoWork $Plugin

  $themeTypes = Join-Path $Plugin.WebDir "node_modules\@devilgenius\airgate-theme\dist\index.d.ts"
  if (-not (Test-Path $themeTypes)) {
    Invoke-PnpmInstall $Plugin.WebDir -Force
  }

  Invoke-InDir $Plugin.WebDir "pnpm build"
  Sync-PluginWebdist $Plugin

  New-Item -ItemType Directory -Force -Path (Join-Path $Plugin.Root "bin") | Out-Null
  Invoke-InDir $Plugin.BackendDir (Get-GoEnvCommand "go build -o ..\bin\$($Plugin.Name) .")
}

function Build-All {
  Assert-Command "pnpm"
  Invoke-CoreUnusedLint
  Invoke-InDir $SdkTheme "pnpm build"

  $themeTypes = Join-Path $WebDir "node_modules\@devilgenius\airgate-theme\dist\index.d.ts"
  if (-not (Test-Path $themeTypes)) {
    Invoke-PnpmInstall $WebDir -Force
  }

  Invoke-InDir $WebDir "pnpm build"
  Sync-Webdist

  foreach ($plugin in $PluginSpecs) {
    Build-Plugin $plugin
  }
}

function Build-BackendDevBinary {
  Invoke-CoreUnusedLint
  Write-Step "building backend dev binary"
  Push-Location $BackendDir
  try {
    Set-GoEnvVars
    & go build -o $BackendDevExe ./cmd/server
    if ($LASTEXITCODE -ne 0) {
      throw "Command failed with exit code ${LASTEXITCODE}: go build -o $BackendDevExe ./cmd/server"
    }
  } finally {
    Pop-Location
  }
}

function Get-ProcessStartTimeUtc([int]$ProcessId) {
  $proc = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
  if (-not $proc) {
    return $null
  }

  try {
    $proc.StartTime.ToUniversalTime()
  } catch {
    $null
  }
}

function Get-ProcessSnapshot([int]$ProcessId) {
  $proc = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
  if (-not $proc) {
    return $null
  }

  $startTime = $null
  try {
    $startTime = $proc.StartTime.ToUniversalTime()
  } catch {
  }

  $path = ""
  try {
    $path = [string]$proc.Path
  } catch {
  }

  [pscustomobject]@{
    pid = $ProcessId
    processName = [string]$proc.ProcessName
    processPath = $path
    startTimeUtc = $startTime
    startTimeTicks = if ($startTime) { $startTime.Ticks } else { $null }
  }
}

function Write-PidFile([string]$PidFile, [int]$ProcessId, [string]$Role) {
  $snapshot = Get-ProcessSnapshot $ProcessId
  $state = [pscustomobject]@{
    pid = $ProcessId
    role = $Role
    processName = if ($snapshot) { $snapshot.processName } else { $null }
    processPath = if ($snapshot) { $snapshot.processPath } else { $null }
    startTimeUtc = if ($snapshot -and $snapshot.startTimeUtc) { $snapshot.startTimeUtc.ToString("O") } else { $null }
    startTimeTicks = if ($snapshot) { $snapshot.startTimeTicks } else { $null }
  }

  $state | ConvertTo-Json -Compress | Set-Content -Path $PidFile
}

function Read-PidFile([string]$PidFile) {
  if (-not (Test-Path $PidFile)) {
    return $null
  }

  $raw = (Get-Content -Raw $PidFile).Trim()
  if ($raw -match "^\d+$") {
    return [pscustomobject]@{
      pid = [int]$raw
      role = $null
      startTimeUtc = $null
      legacy = $true
    }
  }

  try {
    $state = $raw | ConvertFrom-Json
    if ($null -eq $state.pid -or -not ([string]$state.pid -match "^\d+$")) {
      return $null
    }

    [pscustomobject]@{
      pid = [int]$state.pid
      role = [string]$state.role
      processName = [string]$state.processName
      processPath = [string]$state.processPath
      startTimeUtc = [string]$state.startTimeUtc
      startTimeTicks = if ($null -ne $state.startTimeTicks -and [string]$state.startTimeTicks -match "^\d+$") { [int64]$state.startTimeTicks } else { $null }
      legacy = $false
    }
  } catch {
    $null
  }
}

function Test-PidStateMatchesProcess($State) {
  if ($null -eq $State -or -not (Get-Process -Id $State.pid -ErrorAction SilentlyContinue)) {
    return $false
  }

  if ($State.legacy) {
    return $false
  }

  if ([string]::IsNullOrWhiteSpace($State.startTimeUtc)) {
    if ($null -eq $State.startTimeTicks) {
      return $false
    }
  }

  $actual = Get-ProcessSnapshot $State.pid
  if (-not $actual) {
    return $false
  }

  if (-not [string]::IsNullOrWhiteSpace($State.processName) -and $actual.processName -ne $State.processName) {
    return $false
  }

  if (-not [string]::IsNullOrWhiteSpace($State.processPath) -and -not [string]::IsNullOrWhiteSpace($actual.processPath)) {
    if ($actual.processPath -ne $State.processPath) {
      return $false
    }
  }

  if ($null -ne $State.startTimeTicks -and $null -ne $actual.startTimeTicks) {
    return [math]::Abs($actual.startTimeTicks - $State.startTimeTicks) -le ([timespan]::FromSeconds(10).Ticks)
  }

  if (-not [string]::IsNullOrWhiteSpace($State.startTimeUtc) -and $actual.startTimeUtc) {
    try {
      $expected = ([datetime]$State.startTimeUtc).ToUniversalTime()
      return [math]::Abs(($actual.startTimeUtc - $expected).TotalSeconds) -le 10
    } catch {
      return $false
    }
  }

  $false
}

function Stop-ProcessId([int]$ProcessId, [switch]$Tree) {
  if ($ProcessId -eq $PID) {
    Write-Step "refusing to stop current shell pid $ProcessId"
    return
  }
  if (-not (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue)) {
    return
  }

  if ($Tree) {
    & taskkill.exe /PID $ProcessId /T /F 2>$null | Out-Null
  } else {
    Stop-Process -Id $ProcessId -Force -ErrorAction SilentlyContinue
  }

  try {
    Wait-Process -Id $ProcessId -Timeout 3 -ErrorAction SilentlyContinue
  } catch {
  }

  if (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue) {
    Write-Step "pid $ProcessId did not exit within timeout"
  }
}

function Stop-FromPidFile([string]$Name, [string]$PidFile, [switch]$Tree, [int[]]$FallbackPorts = @(), [string[]]$FallbackProcessNames = @()) {
  if (-not (Test-Path $PidFile)) {
    if ($FallbackPorts.Count -gt 0) {
      Write-Step "$Name pid file not found; stopping matching listener ports instead"
      Stop-PortListeners $FallbackPorts -ExpectedProcessNames $FallbackProcessNames
    } else {
      Write-Step "$Name pid file not found"
    }
    return
  }

  $state = Read-PidFile $PidFile
  if (-not $state) {
    Write-Step "$Name pid file is invalid; removing $PidFile"
    Remove-Item -Force $PidFile -ErrorAction SilentlyContinue
    return
  }

  if ($state.legacy -and $FallbackPorts.Count -gt 0) {
    Write-Step "$Name pid file is legacy; stopping matching listener ports instead"
    Stop-PortListeners $FallbackPorts -ExpectedProcessNames $FallbackProcessNames
    Remove-Item -Force $PidFile -ErrorAction SilentlyContinue
    return
  }

  if ($state.legacy) {
    Write-Step "$Name pid file is legacy and cannot be safely validated; removing pid file only"
    Remove-Item -Force $PidFile -ErrorAction SilentlyContinue
    return
  }

  if (-not [string]::IsNullOrWhiteSpace($state.role) -and $state.role -ne $Name) {
    if ($FallbackPorts.Count -gt 0) {
      Write-Step "$Name pid file role mismatch ($($state.role)); stopping matching listener ports instead"
      Stop-PortListeners $FallbackPorts -ExpectedProcessNames $FallbackProcessNames
    } else {
      Write-Step "$Name pid file role mismatch ($($state.role)); removing pid file only"
    }
    Remove-Item -Force $PidFile -ErrorAction SilentlyContinue
    return
  }

  if (Test-PidStateMatchesProcess $state) {
    Write-Step "stopping $Name pid $($state.pid)"
    Stop-ProcessId $state.pid -Tree:$Tree
  } else {
    if ($FallbackPorts.Count -gt 0) {
      Write-Step "$Name pid $($state.pid) is stale or reused; stopping matching listener ports instead"
      Stop-PortListeners $FallbackPorts -ExpectedProcessNames $FallbackProcessNames
    } else {
      Write-Step "$Name pid $($state.pid) is stale or reused; removing pid file only"
    }
  }
  Remove-Item -Force $PidFile -ErrorAction SilentlyContinue
}

function Stop-PortListeners([int[]]$Ports, [string[]]$ExpectedProcessNames = @()) {
  foreach ($port in $Ports) {
    $listeners = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
    foreach ($listener in $listeners) {
      $listenerPid = [int]$listener.OwningProcess
      if ($listenerPid -le 0 -or $listenerPid -eq $PID) {
        continue
      }

      $proc = Get-Process -Id $listenerPid -ErrorAction SilentlyContinue
      if ($ExpectedProcessNames.Count -gt 0 -and (-not $proc -or $ExpectedProcessNames -notcontains $proc.ProcessName)) {
        $processName = if ($proc) { $proc.ProcessName } else { "unknown" }
        Write-Step "skipping listener on port $port pid $listenerPid process $processName"
        continue
      }

      Write-Step "force stopping listener on port $port pid $listenerPid"
      Stop-ProcessId $listenerPid -Tree
    }
  }
}

function Stop-Dev {
  foreach ($plugin in $PluginSpecs) {
    Stop-FromPidFile "$($plugin.Name) web watch" $plugin.WatchPidFile -Tree
  }
  Stop-FromPidFile "frontend" $FrontendPidFile -Tree -FallbackPorts @($FrontendDevPort) -FallbackProcessNames @("node")
  Stop-FromPidFile "backend" $BackendPidFile -Tree -FallbackPorts @(9517) -FallbackProcessNames @("airgate-core-dev", "server")

  if ($ForcePortStop) {
    Stop-PortListeners @($FrontendDevPort, 9517)
  }
}

function Test-PortFree([int]$Port) {
  -not (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue)
}

function Ensure-PluginDists {
  foreach ($plugin in $PluginSpecs) {
    Ensure-PluginGoWork $plugin
    if (-not (Test-Path (Join-Path $plugin.WebDist "index.js")) -or -not (Test-Path (Join-Path $plugin.EmbedDir "index.js"))) {
      Write-Step "$($plugin.Name) webdist is missing; building plugin"
      Build-Plugin $plugin
    }
  }
}

function Start-PluginWatchers {
  foreach ($plugin in $PluginSpecs) {
    Remove-Item -Force $plugin.WatchOut, $plugin.WatchErr -ErrorAction SilentlyContinue
    $watch = Start-Process `
      -FilePath "pwsh" `
      -ArgumentList @("-NoLogo", "-NoProfile", "-Command", "pnpm dev") `
      -WorkingDirectory $plugin.WebDir `
      -WindowStyle Hidden `
      -RedirectStandardOutput $plugin.WatchOut `
      -RedirectStandardError $plugin.WatchErr `
      -PassThru
    Write-PidFile $plugin.WatchPidFile $watch.Id "$($plugin.Name) web watch"
    Write-Step "$($plugin.Name) web watch starting, pid $($watch.Id), logs: $($plugin.WatchOut)"
  }
}

function Start-Dev {
  Assert-Command "pnpm"
  Assert-Command "go"
  Ensure-Dirs

  if ($Install) {
    Install-Deps
  }
  if ($Build) {
    Build-All
  } elseif (-not (Test-Path (Join-Path $WebDistEmbed "index.html"))) {
    Write-Step "embedded webdist is missing; building once"
    Build-All
  }
  Ensure-PluginDists

  if (-not (Test-PortFree 9517)) {
    throw "Port 9517 is already in use. Run: .\deploy\dev.ps1 stop -ForcePortStop"
  }
  if (-not (Test-PortFree $FrontendDevPort)) {
    throw "Port $FrontendDevPort is already in use. Run: .\deploy\dev.ps1 stop -ForcePortStop"
  }

  $configFile = Write-DevConfig
  $configArg = "`"$configFile`""
  Remove-Item -Force $BackendOut, $BackendErr, $FrontendOut, $FrontendErr -ErrorAction SilentlyContinue
  Build-BackendDevBinary
  Start-PluginWatchers

  $backend = Start-Process `
    -FilePath $BackendDevExe `
    -ArgumentList @("--config", $configArg) `
    -WorkingDirectory $BackendDir `
    -WindowStyle Hidden `
    -RedirectStandardOutput $BackendOut `
    -RedirectStandardError $BackendErr `
    -PassThru
  Write-PidFile $BackendPidFile $backend.Id "backend"

  $frontend = Start-Process `
    -FilePath "pwsh" `
    -ArgumentList @("-NoLogo", "-NoProfile", "-Command", "pnpm dev --port $FrontendDevPort --strictPort") `
    -WorkingDirectory $WebDir `
    -WindowStyle Hidden `
    -RedirectStandardOutput $FrontendOut `
    -RedirectStandardError $FrontendErr `
    -PassThru
  Write-PidFile $FrontendPidFile $frontend.Id "frontend"

  Write-Step "backend starting, pid $($backend.Id), logs: $BackendOut"
  Write-Step "frontend starting, pid $($frontend.Id), logs: $FrontendOut"
  Start-Sleep -Seconds 3
  Show-Status
}

function Show-Status {
  $rows = foreach ($port in $FrontendDevPort, 9517) {
    $listeners = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
    if ($listeners) {
      foreach ($listener in $listeners) {
        $proc = Get-Process -Id $listener.OwningProcess -ErrorAction SilentlyContinue
        [pscustomobject]@{
          Port = $port
          Status = "listening"
          PID = $listener.OwningProcess
          Process = $proc.ProcessName
          Address = $listener.LocalAddress
        }
      }
    } else {
      [pscustomobject]@{
        Port = $port
        Status = "free"
        PID = ""
        Process = ""
        Address = ""
      }
    }
  }

  $rows | Format-Table -AutoSize
  Write-Host "Frontend: http://localhost/"
  Write-Host "Backend:  http://127.0.0.1:9517"
  if (Test-Path $DevConfigFile) {
    Write-Host "Config:   $DevConfigFile"
  }
  Write-Host "Backend log:  $BackendOut"
  Write-Host "Frontend log: $FrontendOut"
  foreach ($plugin in $PluginSpecs) {
    $watchState = Read-PidFile $plugin.WatchPidFile
    $watchPid = if ($watchState) { $watchState.pid } else { "" }
    $devLoaded = if (Test-Path (Join-Path $plugin.WebDist "index.js")) { "web dist ok" } else { "web dist missing" }
    Write-Host "$($plugin.Name): $devLoaded, watch pid $watchPid, log: $($plugin.WatchOut)"
  }
}

$PluginSpecs = Get-AvailablePluginSpecs
Assert-Paths
Ensure-Dirs

switch ($Action) {
  "start" {
    Start-Dev
  }
  "stop" {
    Stop-Dev
    Show-Status
  }
  "restart" {
    Stop-Dev
    Start-Sleep -Seconds 1
    Start-Dev
  }
  "status" {
    Show-Status
  }
  "build" {
    if ($Install) {
      Install-Deps
    }
    Build-All
  }
}
