# `app.go` - 主应用容器与模块编排手册 (简体中文)

本文档提供对 **[`app.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/app.go)** 代码的深度说明。

---

## 🏛 系统定位与架构角色

在 Mooncake 2.0 Client 架构中，`app.go` 扮演**主控制引擎容器 (Master Control Container / Dependency Injection Root)**。

`App` 结构体封装了所有核心子系统的指针（Pointers），并管理严格的初始化、顺序启动、并发 Goroutines 与关机清理流程。
