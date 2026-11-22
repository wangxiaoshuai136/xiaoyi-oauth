# Changelog

## v0.3.0 (2026-11-23)

- **纯华为账号登录，不需要安装小艺Work**：插件内置 PKCE OAuth + `127.0.0.1:17355/auth/callback` 回调服务器，授权后自动换 token、创建 businessCredential。
- 登录凭据含 Huawei access/refresh token、businessCredential、uid、deviceId；过期时直接用华为 refresh token 续期。
- 若本机已装过小艺Work，仍保留 keychain 自动导入作为兜底；回调端口被占用时回退到 keychain。
- 登录状态页文案改为「无需安装或启动小艺Work」。

## v0.2.2 (2026-11-22)

- 所有模型声明 `low / medium / high` 思考档位（可关闭），并把 `reasoning_effort` 透传给云端；修复「思考强度不能选」。

## v0.2.1 (2026-11-22)

- 云端非流式请求不再发送 `Accept: application/json`（上游只接受 SSE，会返回 406）。

## v0.2.0 (2026-11-22)

- **不再依赖桌面端运行**：默认直连 `https://hag-drcn.op.dbankcloud.com/celia/v1/sse-api`，使用 `businessCredential` + `x-uid` + `x-device-id` + `x-businessId` 鉴权。
- 登录凭据包含 businessCredential、access/refresh token、clientId、deviceId；401 或到期时通过 refresh-token 网关与 business-credential/create 自动续期。
- `base_url` 设为 `127.0.0.1:19693` 时仍可回退本地代理模式。

## v0.1.3 (2026-11-22)

- 仓库更名为 `wangxiaoshuai136/xiaoyi-oauth`，Go module 与元数据仓库地址同步。
- 新增插件图标 `icon.png`，插件在 ECPA 里的 Logo 改为仓库 raw 图标。

## v0.1.2 (2026-11-22)

- 插件显示名改为 **Xiaoyi OAuth**（provider id 仍是 `xiaoyiwork`，模型前缀不变）。
- 账号从“匿名本地代理”升级为**可登录凭证**：读取 `users/*/identity.json` 的 uid，登录/注册时写入 `oauth/xiaoyiwork-<uid>.json`，包含 `proxy_key`、`key_source`，账号标签为 `Xiaoyi OAuth (uid前4…后4)`。
- 支持 ECPA 的插件登录流程（`auth.login.start/poll/refresh`）：开始登录后打开本地状态页，几秒内自动导入；401/定时刷新时重读桌面端凭据。
- 执行器优先使用凭据里的 proxyKey，缺失时才回退注册表 + DPAPI。
- 新增插件管理页 `/v0/resource/plugins/xiaoyiwork/status` 与状态 API。
- 源码仓库迁至 `wangxiaoshuai136/cpa-plugin-xiaoyiwork`，由 GitHub Actions 构建并发布 Windows DLL。

## v0.1.1 (2026-11-22)

- 修复 v8 内核在请求阶段要求 provider 拥有凭据的问题：改为 `AuthProvider + ExecutorModelScopeOAuth`，插件注册后自动写入免密钥凭据 `oauth/xiaoyiwork-local.json`（只含 `type/provider`，不含 proxyKey）。
- 经本机运行中的 ECPA 8317 网关实测：`/v1/models` 暴露 8 个模型；`/v1/chat/completions` 非流式与流式均通过（含 `[DONE]`、usage、reasoning）；`tools` 返回 `finish_reason=tool_calls`；裸模型名 `LLM_GLM5.3` 可路由；`/v1/responses` 正常返回。

## v0.1.0 (2026-11-22)

首个可用版本。

- 新增 ECPA 原生插件 `xiaoyiwork`：把已登录的小艺Work 桌面端本地模型代理接入网关。
- proxyKey 通过注册表 + DPAPI 在内存内解密，不写任何明文文件；支持配置项与环境变量覆盖。
- 模型目录读取小艺Work 自带 `models.json`，读不到时使用内置回退表；支持前缀模型名与展示名。
- 上游强制流式，插件重写为标准 OpenAI chunk：补 `object`/`created`/`index`/唯一 `id`，合并工具调用片段，结束时补 `data: [DONE]`。
- 非流式请求在插件内聚合为 `chat.completion`；`reasoning_content` 与 usage（含缓存/推理 token 明细）透传。
- 丢弃上游无效参数（`max_tokens`、`temperature`、`stop`、`stream_options` 等），未知模型直接 400，避免静默回落。
- 附带 `cmd/hostsim` 最小宿主模拟器，可在不启动 ECPA 的情况下验证 DLL 的注册、目录与执行器链路。
