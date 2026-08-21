# stop_vllm.ps1 - Stop vLLM background daemon
Write-Host "===================================================" -ForegroundColor Cyan
Write-Host "  Stopping vLLM Windows Background Daemon..." -ForegroundColor Cyan
Write-Host "===================================================" -ForegroundColor Cyan

$Connections = Get-NetTCPConnection -LocalPort 8000 -State Listen -ErrorAction SilentlyContinue

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
    Write-Host "[INFO] No active service detected on Port 8000." -ForegroundColor Yellow
}
