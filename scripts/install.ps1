[CmdletBinding()]
param(
    [string]$DllPath = (Join-Path $PSScriptRoot '..\bin\xiaoyiwork-v0.6.16.dll'),
    [string]$PluginDir = 'D:\WenPaul\Documents\路由网关\EasyCLIProxyAPI\cpa-core\plugins\windows\amd64'
)

$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $DllPath)) {
    throw "找不到插件 DLL：$DllPath"
}
New-Item -ItemType Directory -Force -Path $PluginDir | Out-Null
$target = Join-Path $PluginDir (Split-Path -LiteralPath $DllPath -Leaf)
Copy-Item -LiteralPath $DllPath -Destination $target -Force

Write-Host "已安装：$target"
Write-Host @'

还需要在 cpa-core\config.yaml 的 plugins.configs 下加入：

    xiaoyiwork:
      enabled: true

然后重启内核（或等插件热重载）。确认命令：
  Select-String -LiteralPath '...\cpa-core\logs\main.log' -Pattern 'pluginhost.*xiaoyiwork'
'@
