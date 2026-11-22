# Changelog

## v0.1.2 (2026-11-22)

- 插件显示名改为 **Xiaoyi OAuth**（provider id 仍是 `xiaoyiwork`，模型前缀不变）。
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
