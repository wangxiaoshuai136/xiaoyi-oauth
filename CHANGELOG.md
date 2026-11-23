# Changelog

## v0.4.2 (2026-11-23)

- 免费额度按套餐总量计算：`xy_free_benefit`（小艺体验版）每月 200，192 剩余显示 96%，不再把剩余量当分母显示 100%。
- 赠送/购买/会员/补偿积分按各自数量单独展示。

## v0.4.1 (2026-11-23)

- 额度按来源拆分展示：免费额度 / 赠送积分 / 购买积分 / 会员积分 / 补偿积分，百分比按总积分计算。
- 登录状态页带 `state` 时自动跳转华为账号页，不再停在说明页；插件仍用独立 Edge/Chrome 窗口。

## v0.4.0 (2026-11-23)

- 登录改为插件**自己拉起独立 Edge/Chrome**（临时独立 profile，不碰日常浏览器）；二维码/短信/密码等所有华为登录方式保留。
- 按 ECPA 插件规范返回 `/v0/resource/plugins/...` 地址，GUI 不再用默认浏览器二次打开。
- 移除本地 keychain 自动导入/扫描：凭据只来自华为账号 OAuth 或显式导入，不再读小艺Work 的本地文件。
- 刷新只用凭据内的 businessCredential + refresh token；登录完成立即落盘，额度卡使用新凭据。

## v0.3.3 (2026-11-23)

- 授权成功后在回调里**立即写 auth 文件**并更新内存缓存，不再依赖 GUI 下一次轮询；登录完成就能看到账号。
- poll 在指定 state 未完成时自动认领任意已完成会话，旧标签页回调后 GUI 不会一直 pending。
- 华为登录页可直接选“扫码登录”；插件不改变华为页面。
- 登录后额度查询使用新 businessCredential，不再 401。

## v0.3.2 (2026-11-23)

- 新增额度查询：`ability-billing/.../privilege/query` → AI 积分、会员/订阅、有效期；凭据卡不再显示“暂不支持额度查询”。
- 登录回调改为多会话映射，重复点“开始登录”或旧标签页回调不再报 `state 不匹配`。
- 插件 shutdown 时关闭 17355 回调服务器；旧版本热重载留下的监听需要重启一次内核释放。

## v0.3.1 (2026-11-23)

- **纯华为账号登录，不需要安装小艺Work**：插件内置 PKCE OAuth + `127.0.0.1:17355/auth/callback` 回调服务器，授权后自动换 token、创建 businessCredential。
- 登录凭据含 Huawei access/refresh token、businessCredential、uid、deviceId；过期时用 refresh token + businessCredential 续期。
- 若本机已装过小艺Work，仍保留 keychain 自动导入作为兜底。

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
