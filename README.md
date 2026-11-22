# cpa-plugin-xiaoyiwork（Xiaoyi OAuth）

把已登录的 **小艺Work 桌面端**本地模型代理接进 **EasyCLIProxyAPI / CLIProxyAPI（ECPA）**，直接消耗小艺Work 额度，无需单独申请 API Key、无需改小艺Work 本体。

> Windows 专用：proxyKey 存在注册表，主密钥由 DPAPI(CurrentUser) 保护；插件必须与已登录的小艺Work 运行在同一 Windows 用户下。

## 原理（已在本机 0.7.7.1 实测）

小艺Work 自带一个只允许本机访问的 OpenAI 兼容端点：

```
POST http://127.0.0.1:19693/celia/v1/sse-api/chat/completions
X-Api-Key: <proxyKey>
```

- 只放行 `/celia/v1/sse-api/` 前缀，**没有** `/models`、`/embeddings`、`/responses`。
- 无论请求里的 `stream` 是什么，上游都返回 SSE；也**不会**发送 `data: [DONE]`。
- 参数 `max_tokens`、`max_completion_tokens`、`temperature`、`top_p`、`stop`、`n`、`stream_options` 全部被忽略；`tools` / `tool_choice` / `response_format` 可用。
- HTTP 200 也可能是错误：上游会把错误放进 SSE 帧 `{"error":{"code":...,"message":...}}`。
- 传入未知模型不会报错，而是静默回落默认模型；本插件先做白名单校验，未知模型直接 400。

插件做四件事：

1. **取密钥并登录**：读 `HKCU\Software\ClawDesktop\keychain\model-proxy` 的 `cHJveHlLZXk`（REG_SZ，base64 的 Chromium OSCrypt v10 信封），再用 `%APPDATA%\xiaoyiwork\Local State` 里的 DPAPI 主密钥做 AES-256-GCM 解密。登录取到密钥后会把它写进该账号的 OAuth 凭据文件（与其它 OAuth 账号同等待遇）；请求优先使用凭据里的 key，401 时重读注册表。
2. **出目录**：从 `%APPDATA%\xiaoyiwork\users\*\models.json` 读取真实模型表（取最近修改的一份），读不到时用内置回退表。
3. **反代执行**：统一以 `stream:true` 调上游，把私有 SSE 帧重写成标准 OpenAI chunk（补 `object`/`created`/`index`/唯一 `id`），合并工具调用片段，最后补 `data: [DONE]`；非流式请求在插件内聚合为 `chat.completion`。
4. **报错如实**：密钥缺失返回 503，上游 401/403 返回 401，限流返回 429，其余上游失败返回 502，并在消息中保留上游错误文本。

## 安装

1. 确认小艺Work 正在运行并已登录（代理只在 App 运行时监听 `127.0.0.1:19693`）。
2. 把 `xiaoyiwork-v0.1.2.dll` 放到：

```
<EasyCLIProxyAPI 目录>\cpa-core\plugins\windows\amd64\
```

3. 在 `cpa-core\config.yaml` 的 `plugins.configs` 下增加（没有这一项时插件默认不加载）：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    xiaoyiwork:
      enabled: true
```

4. 重启 ECPA 内核，或等插件热重载。日志 `cpa-core\logs\main.log` 出现
   `pluginhost: plugin loaded plugin_id=xiaoyiwork` 即成功。

插件注册后会自动创建账号凭据 `oauth/xiaoyiwork-<uid>.json`（含 `account_id`、`uid`、`proxy_key`、`key_source`），账号名为 `Xiaoyi OAuth (uid前4…后4)`。也可以在 ECPA 里点“开始登录”，插件会打开一个本地状态页并在几秒内完成导入。

也可以在 GUI 的插件页用“本地安装”选择 DLL，再到插件配置里把 `enabled` 打开；配置项会按下面的字段渲染成表单。

## 账号与登录

- **自动导入**：插件注册后读取 `%APPDATA%\xiaoyiwork\users\*\identity.json` 的 `uid`，生成凭据 `oauth/xiaoyiwork-<uid>.json`，并把当前 proxyKey 写进去。
- **手动登录**：在 ECPA 的凭据页点“开始登录”，插件返回本地状态页 URL；只要小艺Work 已登录，3 秒内轮询就会成功并保存账号。
- **账号标签**：`Xiaoyi OAuth (3500…0207)`，可正常启用、禁用、设置排除模型。
- **刷新**：`auth.refresh` 会重读注册表 + DPAPI，返回新的 `proxy_key`；请求遇到 401 时会自动失效重读并重试一次。
- **状态页**：`/v0/resource/plugins/xiaoyiwork/status`；状态 API：`/v0/management/xiaoyiwork/api/state`。

## 配置

| 字段 | 默认值 | 说明 |
|---|---|---|
| `base_url` | `http://127.0.0.1:19693/celia/v1/sse-api` | 本地代理地址，一般不用改 |
| `model_prefix` | `xiaoyiwork/` | 暴露给客户端的模型前缀 |
| `api_key` | 空 | 可选覆盖；留空则自动从注册表 + DPAPI 解密 |
| `route_unprefixed_models` | `true` | 同时路由不带前缀的原生模型 id |
| `request_timeout_seconds` | `600` | 单个请求上限（长思考模型建议保留） |
| `debug` | `false` | 在宿主日志里输出密钥来源等排障信息 |

也可以用环境变量覆盖密钥：`XIAOYIWORK_PROXY_KEY` 或 `DSH_XIAOYIWORK_PROXY_KEY`（优先级低于配置项、高于自动解密）。

## 模型

模型 id 以本机 `models.json` 为准，内置回退表如下（0.7.7.1）：

| 展示名 | 上游 id | 倍率 |
|---|---|---|
| Auto-Model | `Auto-Model` | — |
| DeepSeek-V4-Flash | `LLM_DeepSeekV4_Think` | 1x |
| DeepSeek-V4.1-Flash | `DeepSeek_V4.1_Flash_VE` | 1x |
| openPangu-2.1-Pro | `OpenPanguV2-Pro` | 1x |
| GLM-5.3 | `LLM_GLM5.3` | 8x |
| GLM-5.3-Flash | `GLM_5.3_FLASH_VE` | 0.8x |
| MiniMax M3.1 Flash | `MiniMax_M3.1_Flash` | 1x |
| Kimi K3.1 Pro | `Kimi_K3.1_Pro_VE` | 13x |

客户端使用 `xiaoyiwork/<上游 id>`（例如 `xiaoyiwork/GLM_5.3_FLASH_VE`）。展示名也可以直接作为模型名传入。

## 调用示例

```bash
curl http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer <你的 CPA Key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"xiaoyiwork/GLM_5.3_FLASH_VE","messages":[{"role":"user","content":"Say OK"}],"stream":true}'
```

## 限制与已知行为

### 已在运行中的 ECPA 验证（2026-11-22）

- 插件热重载：`plugin loaded / registered / hot reloaded xiaoyiwork 0.1.1`，未重启内核。
- `/v1/models`：8 个小艺Work 模型；`/v1/chat/completions` 非流式返回 `OK` 并带 usage。
- 流式：标准 role/内容/reasoning chunk + 结束 chunk + usage + `data: [DONE]`。
- `tools`：`finish_reason=tool_calls`，arguments 正确合并。
- 裸模型名（如 `LLM_GLM5.3`）与 `/v1/responses` 均可正常使用。

- 仅 Windows、仅同一登录用户；换用户或做成服务会 DPAPI 解密失败。
- 上游不支持输出上限、温度、停止词等参数；插件会主动丢弃，避免“看起来生效”。
- 上游没有模型列表接口，目录来自 App 的 `models.json`，会缓存 60 秒。
- 倍率高的模型（GLM-5.3 / Kimi K3.1 Pro）会更快消耗小艺Work 额度，请自行控制。
- 小艺Work 升级后 keychain 格式可能变化；当前实现针对 0.7.7.1 验证。

## 排障

```powershell
# 插件是否加载
Select-String -LiteralPath 'D:\WenPaul\Documents\路由网关\EasyCLIProxyAPI\cpa-core\logs\main.log' -Pattern 'pluginhost.*xiaoyiwork'

# 端口是否在听
Get-NetTCPConnection -LocalPort 19693 -State Listen
```

不启动 ECPA 也可以单独验证 DLL：

```powershell
go run .\cmd\hostsim -dll .\bin\xiaoyiwork-v0.1.0.dll -model xiaoyiwork/GLM_5.3_FLASH_VE -prompt "Say OK"
go run .\cmd\hostsim -stream=false -dll .\bin\xiaoyiwork-v0.1.0.dll -model xiaoyiwork/LLM_GLM5.3 -prompt "Say OK"
```

`cmd/hostsim` 实现了最小宿主侧 C ABI，会完整走一遍 `plugin.register → model.static → executor.execute(_stream)`。

## 构建

```powershell
$env:PATH = 'C:\msys64\mingw64\bin;' + $env:PATH
$env:CGO_ENABLED = '1'
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:GOAMD64 = 'v1'
go build -buildmode=c-shared -trimpath -ldflags "-s -w" -o bin/xiaoyiwork-v0.1.2.dll ./cmd/xiaoyiwork-plugin
```

必须是 `-buildmode=c-shared`（不是 Go 标准 `-buildmode=plugin`）；CI 在 Linux 上用 `x86_64-w64-mingw32-gcc` 交叉编译同一个 DLL。

## 与 dsh-xiaoyiwork-connect 的关系

密钥格式、端点路径与 SSE 行为参考了 [moxiaoren/dsh-xiaoyiwork-connect](https://github.com/moxiaoren/dsh-xiaoyiwork-connect)。本仓库是该思路的 ECPA 原生插件实现：Go + C ABI、无需 PowerShell、不写明文密钥，并额外处理了标准 OpenAI 分帧、`[DONE]`、工具调用合并、未知模型白名单和参数丢弃。

## License

MIT
