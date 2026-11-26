<p align="center"><img src="icon.png" width="96" height="96" alt="Xiaoyi OAuth" /></p>

<h1 align="center">Xiaoyi OAuth</h1>

<p align="center">
  华为小艺Work 的 <a href="https://github.com/router-for-me/CLIProxyAPI">CLIProxyAPI</a> 原生插件：纯账号登录、云端直连、模型能力与积分额度一体化。
</p>

<p align="center">
  <a href="https://github.com/wangxiaoshuai136/xiaoyi-oauth/releases"><img src="https://img.shields.io/github/v/release/wangxiaoshuai136/xiaoyi-oauth?label=release" alt="Release"></a>
  <a href="https://github.com/wangxiaoshuai136/xiaoyi-oauth/actions/workflows/release.yml"><img src="https://github.com/wangxiaoshuai136/xiaoyi-oauth/actions/workflows/release.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/CLIProxyAPI-v8.1.1%2B-4f46e5" alt="CLIProxyAPI v8.1.1+">
  <img src="https://img.shields.io/badge/platform-Windows%20amd64-0078d4" alt="Windows amd64">
  <img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT license">
</p>

<p align="center">
  Changelog: <a href="CHANGELOG.md">CHANGELOG.md</a>
</p>

Xiaoyi OAuth 给 CLIProxyAPI（CPA）增加 `xiaoyiwork` 供应商：用华为账号登录后，插件直接用云端接口调用小艺Work 的模型，不需要安装或运行小艺Work 桌面端，也不依赖任何企业 CLI 客户端。插件负责凭据续期、模型目录与能力下发、SSE → 标准 OpenAI chunk 转换、非流式聚合、工具调用合并、错误映射和积分额度。

> [!WARNING]
> 本项目仅供学习与技术交流，禁止商用或违法使用。项目与华为及任何商标持有方无关联，使用后果由使用者自行承担。详见文末免责声明。

## 特性

- **纯账号登录**：华为账号 OAuth（PKCE），支持扫码、短信、密码；无需小艺Work 桌面端。
- **默认浏览器 + 独立窗口**：读取 Windows 默认浏览器；Chrome/Edge/Brave/Vivaldi/Opera 等 Chromium 系使用全新临时 profile，不碰日常浏览器，也不复用登录态，可连续登录多个华为账号。
- **云端直连**：默认走 `hag-drcn.op.dbankcloud.com`，无需本地 19693 代理。
- **模型能力下发**：上下文窗口与最大输出随 `/v1/models` 下发，来源为 OpenRouter 公开模型目录（与小艺桌面端同源），6 小时缓存，失败回退内置表。
- **标准协议**：OpenAI Chat Completions 输入/输出；流式补 `object`/`created`/`index`/唯一 `id`、合并 `tool_calls`、结束补 usage 与 `[DONE]`；非流式在插件内聚合；`/v1/responses` 由内核转换。
- **思考档位**：模型声明 `low / medium / high`，允许关闭；`reasoning_effort` 透传云端，`reasoning_content` 保留。
- **凭据管理**：登录成功立即落盘 `oauth/xiaoyiwork-<uid>.json`；6 小时或 401 自动续期；账号备注、`disabled`、`excluded_models` 等宿主字段在刷新/重载后保留。
- **额度卡片**：读取 billing `privilege/query`，按免费/赠送/购买/会员/补偿拆分积分，显示套餐与自然月刷新时间。
- **空错误帧兜底**：`GLM-5.3-Flash` 在长结构化生成中可能被上游中断并返回 `data: {"error":{}}`；可选 `auto_fallback` 自动改用 `LLM-GLM5.3` 重跑（默认关闭）。

## 登录与凭据

1. 在 ECPA 凭据页点“开始登录”。
2. 插件拉起系统默认浏览器的独立登录窗口（Chromium 系使用临时 profile），打开华为账号页。
3. 扫码 / 短信 / 密码完成登录，浏览器回调到 `http://127.0.0.1:17355/auth/callback`。
4. 插件用授权码换取华为 token，创建 businessCredential，并立即写入 `oauth/xiaoyiwork-<uid>.json`。
5. 账号卡显示 `Xiaoyi OAuth (昵称)`，无需等 GUI 轮询。

注意：

- 小艺云端同一账号同一设备只保留一个有效凭据。若在小艺Work 桌面端重新登录，插件保存的凭据会被顶掉，表现为 401 `authFailed`，需要回 ECPA 重新点“开始登录”。
- 凭据刷新失败（例如 `clientId is invalid`）时同样需要重新登录；登录后额度与聊天立即恢复。
- 默认浏览器不是 Chromium（例如 Firefox）时，插件交给系统默认处理器打开，无法使用临时 profile 隔离。

## 支持的模型

| 展示名 | 上游 id | 上下文窗口 | 最大输出 |
|---|---|---:|---:|
| Auto-Model | `Auto-Model` | 1,048,576 | 131,072 |
| DeepSeek-V4-Flash | `LLM_DeepSeekV4_Think` | 1,048,576 | 393,216 |
| DeepSeek-V4.1-Flash | `DeepSeek_V4.1_Flash_VE` | 1,048,576 | 393,216 |
| openPangu-2.1-Pro | `OpenPanguV2-Pro` | 524,288 | 131,072 |
| GLM-5.3 | `LLM_GLM5.3` | 1,048,576 | 943,718 |
| GLM-5.3-Flash | `GLM_5.3_FLASH_VE` | 1,048,576 | 943,718 |
| MiniMax M3.1 Flash | `MiniMax_M3.1_Flash` | 1,048,576 | 524,288 |
| Kimi K3.1 Pro | `Kimi_K3.1_Pro_VE` | 1,048,576 | 943,718 |

能力解析说明：小艺远端的模型目录接口（`user-ability-gallery/v1/agent/user/query`）只返回名称、描述和积分倍率，不包含上下文；插件运行时从 OpenRouter `/api/v1/models` 按模型家族取值（与小艺桌面端 `enable_openrouter_model_context_window_tokens` 同源），失败时回退内置表。客户端模型设置里仍可按模型覆盖。

## 安装

1. 把 `xiaoyiwork-v0.6.7.dll` 放到 `<EasyCLIProxyAPI 目录>\cpa-core\plugins\windows\amd64\`。
2. 在 `cpa-core\config.yaml` 的 `plugins.configs` 下启用：

```yaml
plugins:
  enabled: true
  configs:
    xiaoyiwork:
      enabled: true
```

3. 重启内核或等热重载；日志出现 `pluginhost: plugin registered plugin_id=xiaoyiwork ... Xiaoyi OAuth` 即成功。

## 配置

| 字段 | 默认值 | 说明 |
|---|---|---|
| `base_url` | `https://hag-drcn.op.dbankcloud.com/celia/v1/sse-api` | 云端直连地址；填 `127.0.0.1:19693` 可改走桌面端本地代理 |
| `model_prefix` | `xiaoyiwork/` | 暴露给客户端的模型前缀 |
| `api_key` | 空 | 仅本地模式需要的 proxyKey 覆盖 |
| `route_unprefixed_models` | `true` | 同时路由裸模型 id |
| `request_timeout_seconds` | `600` | 单个上游请求超时 |
| `debug` | `false` | 宿主日志详细输出 |
| `auto_fallback` | `false` | GLM-5.3-Flash 返回空错误帧时，非流式请求自动改用 `LLM_GLM5.3` 重跑一次（积分倍率更高） |

## 使用

```bash
curl http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer <CPA API Key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"xiaoyiwork/GLM_5.3_FLASH_VE","messages":[{"role":"user","content":"只回复 OK"}],"stream":false}'
```

```bash
curl "http://127.0.0.1:8317/v1/models?client_version=0.171.0-alpha.13" \
  -H "Authorization: Bearer <CPA API Key>"
```

流式请求返回标准 OpenAI chunk；`tools` 返回 `finish_reason=tool_calls` 并合并 arguments；非流式返回 `chat.completion` 与 usage。

## 额度

插件实现 `quota_provider`：从 billing `privilege/query` 读取 AI 积分，按免费额度 / 赠送 / 购买 / 会员 / 补偿拆分，百分比按总积分计算；体验版免费额度按每月 200、每月 1 日 00:00 刷新显示。

## 排障

- **401 `authFailed`**：插件保存的 businessCredential 已被新的登录/刷新替换或失效（常见于桌面端重新登录）。回 ECPA 点“开始登录”。
- **502「上游返回空错误帧」**：`GLM-5.3-Flash` 在长结构化生成中可能跑到上千个 reasoning 帧后被上游中断，原始帧为 `data: {"error":{}}`。开启 `auto_fallback` 后非流式请求会自动改用 `LLM-GLM5.3`；流式不会中途换模型。
- **502 `11115 server inner` / `11111 Cp server response error`**：上游模型侧错误，通常与并发或模型状态有关；`request-retry` 过高会把一次失败放大成大量 502 记录，建议 3–5。
- **`回调端口 17355 被占用`**：旧版本热重载留下的监听；重启一次 ECPA 内核即可，0.6.0 起插件 shutdown 会主动释放。
- **登录窗口一闪而过**：0.6.7 起已修复（Chrome 启动器提前退出被误判为窗口关闭）。
- **模型设置显示 128k 默认模板**：0.6.4 起随 `/v1/models` 下发真实上下文；重新打开模型设置即可。

## 开发

```powershell
go test ./internal/...
go vet ./internal/...

$env:PATH = 'C:\msys64\mingw64\bin;' + $env:PATH
$env:CGO_ENABLED = '1'; $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:GOAMD64 = 'v1'
go build -buildmode=c-shared -trimpath -ldflags "-s -w" -o bin/xiaoyiwork-v0.6.7.dll ./cmd/xiaoyiwork-plugin
```

必须使用 `-buildmode=c-shared`；CI 在 Linux 上用 `x86_64-w64-mingw32-gcc` 交叉编译。

`cmd/hostsim` 提供最小宿主 C ABI，可脱离 ECPA 验证注册、模型目录与流式/非流式执行：

```powershell
go build -o bin/hostsim.exe ./cmd/hostsim
.\bin\hostsim.exe -dll .\bin\xiaoyiwork-v0.6.7.dll -stream=false -prompt ping
```

### 项目结构

```text
cmd/xiaoyiwork-plugin   插件入口（C ABI 导出）
cmd/hostsim             最小宿主模拟器
internal/cpa            插件能力：模型、执行器、登录、额度、配置
internal/xiaoyi         小艺云端客户端、目录、能力解析、密钥
internal/pluginabi      C ABI 常量
internal/pluginapi      插件 API 类型
scripts/install.ps1     本地安装脚本
store/registry-entry.json
```

## License and disclaimer

MIT License. See [LICENSE](LICENSE).

本项目与华为、小艺Work 及任何商标持有方无关联，未获其授权或背书。仅供学习、研究与技术交流，禁止商业使用或任何违法用途。使用本插件可能违反上游服务条款并导致账号受限，风险由使用者自行承担。作者不对因使用本项目造成的任何直接或间接损失负责。
