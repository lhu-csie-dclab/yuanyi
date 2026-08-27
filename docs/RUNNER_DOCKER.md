# Process Management, Docker, & Ray Orchestration Guide

This document details process and container orchestration implemented in [`runner.go`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/runner.go), [`Dockerfile`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/Dockerfile), and [`docker-compose.yml`](file:///c:/Users/chich/Documents/vllm/mooncake2.0-client%20-%2020260818/docker-compose.yml).

---

## 🏃 Process Management (`runner.go`)

`Runner` manages the lifecycle of the Ray Head cluster and vLLM inference engine processes.

### Execution Modes

1. **Direct Mode (Native All-in-One Container)**:
   - Triggered when `ALL_IN_ONE=true`, `DIRECT_MODE=true`, or `/opt/dynamo/venv/bin/vllm` exists.
   - Go agent acts as **PID 1**, natively executing Ray and vLLM commands via `os/exec`.
2. **Container Mode (Host Docker CLI)**:
   - Triggered on host systems without pre-baked vLLM environments.
   - Launches containers via `docker run -d` and `docker exec`.

---

## 🛠️ Docker Container Build (`Dockerfile`)

The project uses a 3-stage Docker build. `web.go` embeds the dashboard's compiled
assets via `//go:embed web-ui/dist`, so the Vue frontend must be built into
`web-ui/dist` *before* the Go build runs — hence the dedicated `web-builder` stage:

```dockerfile
# Stage 0: build the Vue + Vite + Tailwind dashboard
FROM node:22-alpine AS web-builder
WORKDIR /web
COPY web-ui/package.json web-ui/package-lock.jso[n] ./
RUN npm ci
COPY web-ui/ .
RUN npm run build

# Stage 1: Build static Go agent binary
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# //go:embed web-ui/dist requires the directory to exist at build time
COPY --from=web-builder /web/dist ./web-ui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o client .

# Stage 2: Combined Runtime (CUDA 13 + vLLM + Ray + Go Client)
FROM nvcr.io/nvidia/ai-dynamo/vllm-runtime:1.2.1-cuda13
RUN pip install --no-cache-dir "ray[default,adag]" "mooncake-transfer-engine-cuda13==0.3.10.post2"
WORKDIR /app
COPY --from=builder /app/client /app/client

EXPOSE 50007 50006 8100 8998
ENTRYPOINT ["/app/client"]
```

---

## 🐳 Docker Compose Stack (`docker-compose.yml`)

```yaml
version: '3.8'

services:
  yuanyi-client:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: yuanyi_client_node
    network_mode: "host"
    ipc: "host"
    shm_size: "16gb"
    environment:
      - ALL_IN_ONE=true
      - DIRECT_MODE=true
      - NCCL_SOCKET_IFNAME=${IFACE:-eth0}
      - GLOO_SOCKET_IFNAME=${IFACE:-eth0}
    volumes:
      - ./config.json:/app/config.json
      - ${ABS_MODEL_PATH}:/data/model
      - ./mooncake.json:/data/mooncake.json
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: all
              capabilities: [gpu]
    restart: unless-stopped
```

### Critical Container Settings
- `network_mode: "host"`: Binds container ports directly to host network interface.
- `ipc: "host"` & `shm_size: "16gb"`: Required for zero-copy GPU shared memory and multi-GPU NCCL inter-process communication.
- `reservations.devices`: Passes host NVIDIA GPUs into the container via NVIDIA Container Toolkit.
