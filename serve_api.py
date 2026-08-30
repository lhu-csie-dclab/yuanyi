# Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
# SPDX-License-Identifier: Apache-2.0
#
# OpenAI-compatible vLLM API server entrypoint for the native Windows path.
# Launched as a subprocess by runner.go (startVLLMWindows) or standalone via
# start_vllm.ps1. Requires a local vLLM Windows wheel -- see
# docs/install/windows/README.md.

import os
import sys
import tempfile

# 設置 Windows 環境相容性變數
os.environ["USE_LIBUV"] = "0"
os.environ["VLLM_USE_V1"] = "0"
os.environ["CUDA_VISIBLE_DEVICES"] = "0"
os.environ["HF_HUB_DISABLE_SYMLINKS_WARNING"] = "1"
os.environ["VLLM_WORKER_MULTIPROC_METHOD"] = "spawn"

# 自動定位 cudart64_12.dll
venv_dir = os.path.dirname(os.path.dirname(sys.executable))
cudart_path = os.path.join(venv_dir, "Lib", "site-packages", "torch", "lib", "cudart64_12.dll")
if os.path.exists(cudart_path):
    os.environ["VLLM_CUDART_SO_PATH"] = cudart_path

# 排除本機倉庫原始碼目錄以免干擾 site-packages
for p in list(sys.path):
    if p.endswith("vllm-windows") or os.path.exists(os.path.join(p, "CMakeLists.txt")):
        sys.path.remove(p)

# 自動修補 Windows PyTorch 2.6 TCPStore 格式化缺陷
#
# NOTE: torch.distributed.rendezvous._create_c10d_store is a private (underscore-
# prefixed) PyTorch internal with no compatibility guarantee. If a future PyTorch
# release renames/removes it, this silently stops patching and the exact Windows
# TCPStore bug this exists to work around will resurface -- print a warning (not a
# bare `pass`) so that failure has a visible clue pointing back here instead of
# looking like an unrelated crash inside torch.distributed.
try:
    import torch.distributed.rendezvous as rdzv
    import torch.distributed as dist
    orig_create = rdzv._create_c10d_store
    def safe_create(*args, **kwargs):
        try:
            return orig_create(*args, **kwargs)
        except RuntimeError:
            port = args[1] if len(args) > 1 else kwargs.get("port", 29500)
            world_size = args[3] if len(args) > 3 else kwargs.get("world_size", 1)
            store_path = os.path.join(tempfile.gettempdir(), f"c10d_store_{port}.tmp")
            return dist.FileStore(store_path, world_size)
    rdzv._create_c10d_store = safe_create
except Exception as e:
    print(f"[Warning] Could not apply Windows TCPStore compatibility patch ({e}); "
          f"if startup fails with a TCPStore/rendezvous error, this is likely why.",
          file=sys.stderr)

if __name__ == "__main__":
    import winloop as uvloop_impl
    from vllm.utils import FlexibleArgumentParser
    from vllm.entrypoints.openai.cli_args import make_arg_parser, validate_parsed_serve_args
    from vllm.entrypoints.openai.api_server import run_server, cli_env_setup

    default_args = [
        "--model", "Qwen/Qwen3-4B-AWQ",
        "--quantization", "awq",
        "--gpu-memory-utilization", "0.95",
        "--max-model-len", "16384",
        "--max-num-seqs", "32",
        "--swap-space", "16",
        "--cpu-offload-gb", "0",
        "--trust-remote-code",
        "--enforce-eager",
        "--disable-frontend-multiprocessing",
        "--port", "8100",  # matches config.go's VLLM.Port default, not vLLM's own convention --
                            # start_vllm.ps1/status_vllm.ps1/stop_vllm.ps1 assume this port too
        "--host", "0.0.0.0"
    ]
    
    cmd_args = sys.argv[1:] if len(sys.argv) > 1 else default_args
    if "--disable-frontend-multiprocessing" not in cmd_args:
        cmd_args.append("--disable-frontend-multiprocessing")

    cli_env_setup()
    parser = FlexibleArgumentParser(description="vLLM OpenAI-Compatible RESTful API server.")
    parser = make_arg_parser(parser)
    args = parser.parse_args(cmd_args)
    validate_parsed_serve_args(args)

    print(f"\n=======================================================")
    print(f"  vLLM OpenAI 相容 API 伺服器啟動中...")
    print(f"  模型: {args.model}")
    print(f"  位址: http://{args.host}:{args.port}")
    print(f"=======================================================\n")

    uvloop_impl.run(run_server(args))
