<p align="center"><img src="icon.png" width="96" height="96" alt="Xiaoyi OAuth" /></p>

# Xiaoyi OAuth（cpa-plugin-xiaoyiwork）

把已登录的 **小艺Work** 账号接进 **EasyCLIProxyAPI / CLIProxyAPI（ECPA）**。首次从桌面端导入一次登录态，之后**不需要小艺Work 保持运行**，插件直接调用小艺云端模型接口。

> Windows 专用：登录态存在 `HKCU\Software\ClawDesktop\keychain`，主密钥由 DPAPI(CurrentUser) 保护。

## 原理（0.7.7.1 实测）

```
POST https://hag-drcn.op.dbankcloud.com/celia/v1/sse-api/chat/completions
businessCredential: <business-credential>
x-uid: <uid>
x-device-id: <sha256(MachineGuid)>_<uid>
x-businessId: WINPC_XIAOYI_WORK
x-request-from: xiaoyiWork
```

- 凭据来自桌面端的 keychain：`business-credential` 加密值 + `auth-session`（access/refresh/id token）。
- 云端没有 `/models`、`/embeddings`、`/responses`；无论 `stream` 取值都返回 SSE，并且**不发 `[DONE]`**。
- 参数 `max_tokens`、`temperature`、`top_p`、`stop`、`n`、`stream_options` 被忽略；`tools` / `tool_choice` / `response_format` / `reasoning_effort` 可用。
- 凭证过期时：用 `refresh-token` 网关换新 UAT，再调 `business-credential/create` 换新 businessCredential，全程不需要桌面端。
- 本地模式仍保留：把 `base_url` 设为 `http://127.0.0.1:19693/celia/v1/sse-api` 即可走桌面端代理。

插件负责：导入账号、刷新凭据、模型目录、SSE → 标准 OpenAI chunk（补 `object`/`created`/`index`/唯一 `id`、合并工具调用、结束补 `[DONE]`）、非流式聚合、错误映射。

## 安装

1. 把 `xiaoyiwork-v0.2.2.dll` 放到 `<EasyCLIProxyAPI 目录>\cpa-core\plugins\windows\amd64\`。
2. 在 `cpa-core\config.yaml` 的 `plugins.configs` 下启用：

```yaml
plugins:
  enabled: true
  configs:
    xiaoyiwork:
      enabled: true
```

3. 重启内核或等热重载；日志出现 `pluginhost: plugin registered plugin_id=xiaoyiwork ... Xiaoyi OAuth` 即成功。

## 账号与登录

- **首次导入**：在小艺Work 里登录一次，插件注册时会自动读取 keychain，生成 `oauth/xiaoyiwork-<uid>.json`（含 businessCredential、access/refresh token、deviceId）。
- **之后**：小艺Work 可以退出；插件会直接走云端，并在 401/到期时自动刷新凭据。
- **手动登录**：在 ECPA 凭据页点“开始登录”，插件返回本地状态页，几秒内完成导入。
- **账号标签**：`Xiaoyi OAuth (温保禄)`（取 id_token 的 given_name/nickname；没有时用 uid 前后各 4 位）。
- **刷新周期**：`auth.refresh` 每 6 小时或 401 时刷新；refresh token 失效才需要重新在小艺Work 登录。

## 配置

| 字段 | 默认值 | 说明 |
|---|---|---|
| `base_url` | `https://hag-drcn.op.dbankcloud.com/celia/v1/sse-api` | 默认云端直连；填 `127.0.0.1:19693` 改走桌面端 |
| `model_prefix` | `xiaoyiwork/` | 模型前缀 |
| `api_key` | 空 | 仅本地模式需要的 proxyKey 覆盖 |
| `route_unprefixed_models` | `true` | 同时路由裸模型 id |
| `request_timeout_seconds` | `600` | 单请求超时 |
| `panel_base_url` | `http://127.0.0.1:8317` | 登录状态页地址 |
| `debug` | `false` | 宿主日志详细输出 |

## 模型与思考强度

| 展示名 | 上游 id |
|---|---|
| Auto-Model | `Auto-Model` |
| DeepSeek-V4-Flash | `LLM_DeepSeekV4_Think` |
| DeepSeek-V4.1-Flash | `DeepSeek_V4.1_Flash_VE` |
| openPangu-2.1-Pro | `OpenPanguV2-Pro` |
| GLM-5.3 | `LLM_GLM5.3` |
| GLM-5.3-Flash | `GLM_5.3_FLASH_VE` |
| MiniMax M3.1 Flash | `MiniMax_M3.1_Flash` |
| Kimi K3.1 Pro | `Kimi_K3.1_Pro_VE` |

所有模型都声明了 `low / medium / high` 思考档位并允许关闭；请求里的 `reasoning_effort` 会原样透传给云端。

## 已验证（2026-11-22）

- **小艺Work 已退出、19693 未监听**时：8 个模型全部返回 200，非流式 `content=OK`。
- 流式：标准 chunk + usage + `data: [DONE]`。
- `tools`：`finish_reason=tool_calls`，arguments 正确合并。
- `reasoning_effort=high`：正常返回 reasoning_content。
- 裸模型名与 `/v1/responses` 正常。

## 排障

- 看到 502「连接小艺Work 本地代理失败」是 0.1.x 本地模式在桌面端关闭时的旧行为；0.2.x 默认云端直连后不再出现。
- 100% 失败统计可能包含旧版本配合 `request-retry: 200` 产生的重试风暴；修复后新请求正常。
- 401/403：refresh token 失效，重新在小艺Work 登录一次，再点插件的“开始登录”。

## 构建

```powershell
$env:PATH = 'C:\msys64\mingw64\bin;' + $env:PATH
$env:CGO_ENABLED = '1'; $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:GOAMD64 = 'v1'
go build -buildmode=c-shared -trimpath -ldflags "-s -w" -o bin/xiaoyiwork-v0.2.2.dll ./cmd/xiaoyiwork-plugin
```

必须用 `-buildmode=c-shared`；CI 在 Linux 上用 `x86_64-w64-mingw32-gcc` 交叉编译。

## License

MIT
