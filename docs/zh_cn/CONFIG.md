# 配置管理与参数技术手册 (简体中文)

本文档为 Mooncake 2.0 Client Agent 的配置管理提供详细说明，涵盖 [`config.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.go)、[`config.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/config.json) 与 [`mooncake.json`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/mooncake.json)。

---

> [!WARNING]
> **生产环境部署警告与未测试参数声明**：
> 本软件目前处于**实验研究阶段**，**不推荐在正式生产环境 (Production) 部署**。目前仅基准设置（`Qwen3-4B-AWQ`, `protocol: "tcp"`）经测试验证，其余未经测试的参数可能产生不可预期的行为。

---

## 🍰 `mooncake.json` 传输协议设置 (`protocol: "tcp"`)

```json
{
  "metadata_server": "P2PHANDSHAKE",
  "global_segment_size": "0",
  "local_buffer_size": "17179869184",
  "protocol": "tcp",
  "device_name": ""
}
```
