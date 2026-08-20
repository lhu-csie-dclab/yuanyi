# OpenAI API 网关与 Local-First 代理分发器手册 (简体中文)

本文档详细说明 [`proxy.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/proxy.go) 的核心运作机制与 OpenAI 网关。

> [!NOTE]
> 本机是否处理请求现在取决于 `localBusy`（`atomic.Bool` + `CompareAndSwap`）这个并发感知的本机
> 名额，而不只是本机是否健康：单一请求仍走本机快速路径，但并发的第二笔请求会直接分派给远端
> Peer。完整英文版：[`docs/GATEWAY_PROXY.md`](../GATEWAY_PROXY.md)。
