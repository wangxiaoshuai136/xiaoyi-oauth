# Changelog

## v0.6.10 (2026-11-28)

- 思考档位改为按 OpenRouter 的 `reasoning.supported_efforts` 每模型动态下发，不再统一 `low/medium/high`：
  - DeepSeek-V4.1-Flash：`low/high/max`（可关闭）
  - DeepSeek-V4-Flash：`high/xhigh`（可关闭）
  - GLM-5.3 / GLM-5.3-Flash：`low/high/max`（不可关闭）
  - Kimi K3.1 Pro：`low/high/max`（可关闭）
  - MiniMax M3.1 Flash：`low/medium/high/xhigh/max`
- 客户端传入模型不支持的档位时自动映射到最近档（如 `medium → high`；GLM 收到 `none` 时降为 `low`），避免无效参数。
- 说明：ECPA「模型设置」里的默认三项来自内核的 Codex 模型目录模板，不是小艺接口；该页面本身支持勾选 `max`，可在模型设置中按模型调整。

## v0.6.9 (2026-11-27)

- 识别小艺权益/积分错误码 **111110**：不再显示笼统的 502「用户权益校验失败」，改为 **HTTP 402 `xiaoyiwork_quota_exhausted`**，提示“账号积分/权益不足，请充值或等待额度刷新（体验版每月 1 日 00:00 刷新）”。
- 该错误标记为非重试，避免 `request-retry` 把一次积分不足放大成上百次请求。

## v0.6.8 (2026-11-27)

- 登录回归 CPA 规范：`auth.login.start` 只返回华为授权地址，由 ECPA 按其浏览器设置（默认或用户选定）打开；插件不再自己拉起浏览器、临时 profile 或本地状态页。
- 移除浏览器启动、默认浏览器检测与临时 profile 清理代码；回调仍为 `127.0.0.1:17355`。
- 多账号登录改为复用 ECPA 打开的浏览器会话，不再隔离 profile。

## v0.6.7 (2026-11-27)

- 修复 CI 在 Linux 上判断浏览器文件名的问题（`C:\...\msedge.exe` 被当成完整文件名），Windows 行为与 0.6.6 相同。
- 0.6.6 的 Release 因该测试失败未发布；功能以 0.6.7 为准。

## v0.6.6 (2026-11-27)

- 登录窗口改用**系统默认浏览器**，不再写死 Edge：读取 Windows `http` 关联的 ProgId 与打开命令；Chrome/Edge/Brave/Vivaldi/Opera 等 Chromium 系仍用临时 profile 打开独立窗口，Firefox 等其他浏览器交给系统默认处理器打开。
- OAuth 流程不变：仍返回本地登录入口、由插件拉起窗口、回调到 `127.0.0.1:17355`。
- 修复 Chrome 启动器提前退出被误判为“窗口已关闭”，导致登录窗口 2 秒后被自动杀掉；现在只在登录完成、失败或超时时关闭，并按临时 profile 精确清理残留进程。

## v0.6.5 (2026-11-27)

- 修复 OAuth 卡片标题重复：插件显示名仍是 `Xiaoyi OAuth`，但 OAuth 入口名改为 `Xiaoyi`，界面拼接后显示为 **Xiaoyi OAuth**，不再出现 “Xiaoyi OAuth OAuth”。
- 账号标签仍为 `Xiaoyi OAuth (温保禄)`。

## v0.6.4 (2026-11-26)

- 上下文窗口改为运行时从 **OpenRouter 公开模型目录**解析（与小艺桌面端 `enable_openrouter_model_context_window_tokens` 同源），缓存 6 小时；网络不可用时回退内置表。
- 修正 MiniMax M3.1 Flash、DeepSeek-V4-Flash 等被低估的窗口：现在 GLM-5.3 / GLM-5.3-Flash / DeepSeek-V4.x / Kimi K3.1 Pro / MiniMax M3.1 Flash 均为 1,048,576；openPangu-2.1-Pro 为 524,288。
- 小艺远端模型目录接口本身只返回名称/描述/积分倍率，不返回上下文；OpenRouter 是桌面端同款解析来源。

## v0.6.3 (2026-11-26)

- 模型能力随 `/v1/models` 动态下发：按模型返回上下文窗口与最大输出，不再统一写死 128k。GLM-5.3 / GLM-5.3-Flash / DeepSeek-V4.1-Flash / Kimi K3.1 Pro 为 1,048,576，openPangu-2.1-Pro / MiniMax M3.1 Flash 为 524,288，DeepSeek-V4-Flash 与 Auto-Model 为 1,000,000。
- 说明：小艺远端模型目录接口只返回名称、描述和积分倍率，不含上下文；这些数值取自模型家族的公开规格（models.dev），客户端模型设置里仍可按模型覆盖。

## v0.6.2 (2026-11-24)

- 修复**账号备注被刷新/重载覆盖**：插件刷新时合并宿主的 Metadata/Attributes，解析凭据时回填 `note`，重写文件时只更新自己拥有的字段。`note`、`priority`、`headers` 等宿主字段不再丢失。
- 重写逻辑统一为“按字段合并”，`disabled`、`excluded_models` 等现有设置同样保留。

## v0.6.1 (2026-11-24)

- 识别上游**空错误帧** `data: {"error":{}}`：`GLM-5.3-Flash` 在长结构化生成中可能输出上千个 reasoning 帧后由上游中断，之前只显示“未说明的错误”。
- 新增 `auto_fallback`（默认关）：非流式请求遇到空错误帧时，自动改用 `LLM_GLM5.3` 重跑一次，并在宿主日志记录 warn；流式请求不中途换模型，避免半截输出。
- 修正文案：云端 `EOF` 不再误报“本地代理未运行”；`xiayiwork` 拼写改为 `xiaoyiwork`。

## v0.6.0 (2026-11-23)

- 账号授权改为**独立登录窗口**：插件拉起 Edge/Chrome，每次登录使用全新临时 profile，不占用日常浏览器，可连续登录多个华为账号。
- 返回的登录地址挂在插件自己的 17355 服务上（含 `/v0/resource/plugins/`），ECPA 不会再用默认浏览器二次打开；没有 Edge/Chrome 时才回退。
- 登录完成、失败或手动关闭窗口后，结束整个浏览器进程树并删除临时 profile，不再在 `%TEMP%` 残留 20 MB+ 的配置目录。
- 无空白管理页/菜单，登录窗口里保留华为全部登录方式。

## v0.5.1 (2026-11-23)

- 额度卡显示刷新时间：免费额度按自然月，每月 1 日 00:00 刷新，并给出下次刷新时间。
- billing `privilege/query` 响应没有 reset 字段（已核对解包代码与抓包日志），因此按套餐的自然月规则推断。

## v0.5.0 (2026-11-23)

- 账号授权遵循 ECPA 的浏览器设置：插件只返回华为账号页地址，由 ECPA 打开默认/选定浏览器，不再强开 Edge。
- 移除无内容的管理页/空白前端（`ManagementAPI` 与资源菜单），插件卡片不再出现无用按钮。
- 保留额度卡片：体验版免费额度 192/200（96%）。

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
