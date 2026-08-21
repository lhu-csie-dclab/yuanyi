# start_vllm.ps1 - Start vLLM background daemon
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$VenvPython = Join-Path $ScriptDir "vllm-windows\.venv\Scripts\pythonw.exe"
if (-not (Test-Path $VenvPython)) {
    $VenvPython = Join-Path $ScriptDir "vllm-windows\.venv\Scripts\python.exe"
}
$ServeScript = Join-Path $ScriptDir "serve_api.py"

Write-Host "===================================================" -ForegroundColor Cyan
Write-Host "  Starting vLLM Windows Background Daemon..." -ForegroundColor Cyan
Write-Host "===================================================" -ForegroundColor Cyan

# Check if port 8000 is already active
$PortInUse = Get-NetTCPConnection -LocalPort 8000 -State Listen -ErrorAction SilentlyContinue
if ($PortInUse) {
    Write-Host "[INFO] vLLM is already running on Port 8000!" -ForegroundColor Yellow
    Write-Host "To restart, run: .\stop_vllm.ps1 first." -ForegroundColor Yellow
    exit 0
}

# Start process in background without window
Start-Process -FilePath $VenvPython -ArgumentList "`"$ServeScript`"" -WorkingDirectory $ScriptDir -WindowStyle Hidden

Write-Host "Daemon process launched in background. Waiting for initialization..." -ForegroundColor Green
$MaxRetries = 35
$Ready = $false

for ($i = 1; $i -le $MaxRetries; $i++) {
    Start-Sleep -Seconds 2
    try {
        $res = Invoke-RestMethod -Uri "http://127.0.0.1:8000/v1/models" -TimeoutSec 2 -ErrorAction Stop
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
    Write-Host "[API URL] http://localhost:8000/v1" -ForegroundColor Yellow
    Write-Host "[MODEL]   Qwen/Qwen2.5-3B-Instruct-AWQ" -ForegroundColor Yellow
    Write-Host "You can close all terminal windows; the service remains active." -ForegroundColor Cyan
} else {
    Write-Host "[NOTICE] Model is still initializing. Check status anytime with: .\status_vllm.ps1" -ForegroundColor Yellow
}
