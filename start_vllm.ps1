# Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
# SPDX-License-Identifier: Apache-2.0
# start_vllm.ps1 - Start vLLM background daemon
#
# $Port must match serve_api.py's hardcoded --port default (8100, same as
# config.json's vllm.port default). serve_api.py's arg handling replaces its
# entire default argument list wholesale when any CLI args are passed, so
# this script intentionally does NOT pass --port through -- doing so would
# drop --model/--quantization/--host/etc. and fail to start. If you need a
# different port, edit the "--port" value in serve_api.py's default_args and
# the $Port value below together.
$Port = 8100
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$VenvPython = Join-Path $ScriptDir "vllm-windows\.venv\Scripts\pythonw.exe"
if (-not (Test-Path $VenvPython)) {
    $VenvPython = Join-Path $ScriptDir "vllm-windows\.venv\Scripts\python.exe"
}
$ServeScript = Join-Path $ScriptDir "serve_api.py"

Write-Host "===================================================" -ForegroundColor Cyan
Write-Host "  Starting vLLM Windows Background Daemon..." -ForegroundColor Cyan
Write-Host "===================================================" -ForegroundColor Cyan

# Check if the port is already active
$PortInUse = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
if ($PortInUse) {
    Write-Host "[INFO] vLLM is already running on Port $Port!" -ForegroundColor Yellow
    Write-Host "To restart, run: .\stop_vllm.ps1 first." -ForegroundColor Yellow
    exit 0
}

# Start process in background without window (no CLI args -- see note above,
# this relies on serve_api.py's own default_args, which include --port $Port)
Start-Process -FilePath $VenvPython -ArgumentList "`"$ServeScript`"" -WorkingDirectory $ScriptDir -WindowStyle Hidden

Write-Host "Daemon process launched in background. Waiting for initialization..." -ForegroundColor Green
$MaxRetries = 35
$Ready = $false

for ($i = 1; $i -le $MaxRetries; $i++) {
    Start-Sleep -Seconds 2
    try {
        $res = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/v1/models" -TimeoutSec 2 -ErrorAction Stop
        if ($res.data) {
            $Ready = $true
            break
        }
    } catch {
        Write-Host -NoNewline "."
    }
}

Write-Host ""
if ($Ready) {
    Write-Host "[SUCCESS] vLLM Daemon is running permanently in background!" -ForegroundColor Green
    Write-Host "[API URL] http://localhost:$Port/v1" -ForegroundColor Yellow
    Write-Host "[MODEL]   cyankiwi/Qwen3-VL-4B-Instruct-AWQ-4bit" -ForegroundColor Yellow
    Write-Host "You can close all terminal windows; the service remains active." -ForegroundColor Cyan
} else {
    Write-Host "[NOTICE] Model is still initializing. Check status anytime with: .\status_vllm.ps1" -ForegroundColor Yellow
}
