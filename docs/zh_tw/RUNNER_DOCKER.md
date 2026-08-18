# 進程管理、Docker 與 Ray 編排手冊

本文件說明 [`runner.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/runner.go)、[`Dockerfile`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/Dockerfile) 與 [`docker-compose.yml`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/docker-compose.yml) 的進程與容器管理機制。

---

## 🏃 核心模式

- **Direct Mode（原生容器內模式）**：`ALL_IN_ONE=true` 時，Go Agent 作為 PID 1 原生啟動 `/opt/dynamo/venv/bin/ray` 與 `/opt/dynamo/venv/bin/vllm serve`。
- **Container Mode（宿主機模式）**：透過宿主機 Docker CLI (`docker run`) 啟動服務。
