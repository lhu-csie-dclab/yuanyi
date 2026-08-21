# status_vllm.ps1 - check vLLM background status
#
# Must match serve_api.py's --port default (8100) and start_vllm.ps1's $Port.
$Port = 8100

Write-Host "===================================================" -ForegroundColor Cyan
Write-Host "  vLLM Windows Service Status" -ForegroundColor Cyan
Write-Host "===================================================" -ForegroundColor Cyan

$Connections = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue

if ($Connections) {
    $Pids = $Connections | Select-Object -ExpandProperty OwningProcess -Unique
    Write-Host "[STATUS] : RUNNING (Port $Port is open)" -ForegroundColor Green
    Write-Host "[PID]    : $($Pids -join ', ')"

    try {
        $res = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/v1/models" -TimeoutSec 3 -ErrorAction Stop
        Write-Host "[MODEL]  : $($res.data[0].id)" -ForegroundColor Yellow
        Write-Host "[API]    : http://localhost:$Port/v1/chat/completions (Healthy)" -ForegroundColor Green
    } catch {
        Write-Host "[API]    : Initializing / warming up..." -ForegroundColor Yellow
    }
} else {
    Write-Host "[STATUS] : STOPPED (Port $Port is not in use)" -ForegroundColor Red
    Write-Host "To start the background service, run: .\start_vllm.ps1" -ForegroundColor Yellow
}
