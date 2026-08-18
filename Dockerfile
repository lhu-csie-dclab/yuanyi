# Multi-stage build for Mooncake Client Agent
FROM golang:1.26-alpine AS builder


WORKDIR /app

# Copy dependency manifests
COPY go.mod go.su[m] ./
RUN go mod download


# Copy source code
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o client .

# Production image: Combined Runtime (CUDA 13 + vLLM + Ray + Go Client)
FROM nvcr.io/nvidia/ai-dynamo/vllm-runtime:1.2.1-cuda13

# Install Ray and Mooncake transfer engine
RUN pip install --no-cache-dir "ray[default,adag]" "mooncake-transfer-engine-cuda13==0.3.10.post2"

WORKDIR /app

# Copy static Go client executable from builder
COPY --from=builder /app/client /app/client

EXPOSE 50007 50006 8100 8998

# Go agent acts as PID 1 native orchestrator for Ray and vLLM
ENTRYPOINT ["/app/client"]

