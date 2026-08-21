# Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
# SPDX-License-Identifier: Apache-2.0
#
# Multi-stage build for Mooncake Client Agent
#
# NOTE: The production runtime image is based on NVIDIA AI Dynamo vLLM Runtime
# (nvcr.io/nvidia/ai-dynamo/vllm-runtime). Use of that base image is subject
# to the NVIDIA Software License Agreement. See: https://www.nvidia.com/en-us/agreements/enterprise-software/nvidia-software-license-agreement/

# Stage 0: build the Vue + Vite + Tailwind dashboard. Its dist/ output is
# embedded into the Go binary by web.go's //go:embed directive, so it must
# exist before the Go build runs.
FROM node:22-alpine AS web-builder

WORKDIR /web

COPY web-ui/package.json web-ui/package-lock.jso[n] ./
RUN npm ci

COPY web-ui/ .
RUN npm run build

FROM golang:1.26-alpine AS builder


WORKDIR /app

# Copy dependency manifests
COPY go.mod go.su[m] ./
RUN go mod download


# Copy source code
COPY . .

# Bring in the dashboard build from the web-builder stage before compiling,
# since //go:embed web-ui/dist requires the directory to exist at build time.
COPY --from=web-builder /web/dist ./web-ui/dist

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
