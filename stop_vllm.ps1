# Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
# SPDX-License-Identifier: Apache-2.0
# stop_vllm.ps1 - Stop vLLM background daemon
#
# Must match serve_api.py's --port default (8100) and start_vllm.ps1's $Port.
$Port = 8100

Write-Host "===================================================" -ForegroundColor Cyan
Write-Host "  Stopping vLLM Windows Background Daemon..." -ForegroundColor Cyan
Write-Host "===================================================" -ForegroundColor Cyan

$Connections = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue

if ($Connections) {
    $Pids = $Connections | Select-Object -ExpandProperty OwningProcess -Unique
    foreach ($p in $Pids) {
        try {
            $Proc = Get-Process -Id $p -ErrorAction SilentlyContinue
            if ($Proc) {
                Write-Host "Terminating Process ID: $p ($($Proc.ProcessName))..." -ForegroundColor Yellow
                Stop-Process -Id $p -Force -ErrorAction SilentlyContinue
            }
        } catch {}
    }
    Start-Sleep -Seconds 1
    Write-Host "[SUCCESS] vLLM background daemon stopped." -ForegroundColor Green
} else {
    Write-Host "[INFO] No active service detected on Port $Port." -ForegroundColor Yellow
}
