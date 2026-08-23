# Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
# SPDX-License-Identifier: Apache-2.0
#
# Mooncake 2.0 Client Agent -- interactive installer and manager for Windows.
#
# Windows runs the agent natively (Go binary + a local Python/vLLM environment),
# not under Docker, so this is the counterpart to install.sh rather than a port of it.
#
#   irm https://raw.githubusercontent.com/lhu-csie-dclab/yuanyi/main/install.ps1 -OutFile install.ps1
#   powershell -ExecutionPolicy Bypass -File install.ps1

param([string]$Command = "")

$ErrorActionPreference = "Stop"

$RepoUrl = "https://github.com/lhu-csie-dclab/yuanyi.git"

# The wheel/torch pair below is the combination actually verified end to end on this
# project's test hardware (see docs/test/WINDOWS_NATIVE_TEST.md). Newer vllm-windows
# releases exist and are offered during install, but they target CUDA 13.x and need a
# matching driver and torch build, which this project has not validated.
$VerifiedVllmTag   = "v0.9.2"
$VerifiedVllmWheel = "vllm-0.9.2+cu124-cp312-cp312-win_amd64.whl"
$VerifiedTorch     = @("torch==2.6.0+cu124", "torchvision==0.21.0+cu124", "torchaudio==2.6.0+cu124")
$VerifiedTorchIdx  = "https://download.pytorch.org/whl/cu124"

$DefaultModel        = "Qwen/Qwen3-4B-AWQ"
$DefaultBootstrap    = "/dns4/host1.niveec.com/tcp/50004/p2p/12D3KooWBaeTNHHUc1RAePLbYJWvxy9xJXBVyYyW5aEY5hNWfzAh"
$DefaultInstallDir   = Join-Path $HOME "mooncake-client"
$DefaultModelDir     = Join-Path $HOME "mooncake-models"
$DefaultWebPort      = 50007
$DefaultProxyPort    = 50006
$DefaultVllmPort     = 8100
$DefaultMooncakePort = 8998
$DefaultHubP2PPort   = 50004
$DefaultHubProxyPort = 50008

$StateFile = Join-Path $env:APPDATA "mooncake-client\install.json"

function Write-Info { param($m) Write-Host "[*] $m" -ForegroundColor Cyan }
function Write-Ok   { param($m) Write-Host "[+] $m" -ForegroundColor Green }
function Write-Warn { param($m) Write-Host "[!] $m" -ForegroundColor Yellow }
function Write-Err  { param($m) Write-Host "[x] $m" -ForegroundColor Red }
function Fail       { param($m) Write-Err $m; exit 1 }

function Write-Heading {
    param($t)
    Write-Host ""
    Write-Host $t -ForegroundColor White
    Write-Host ("-" * $t.Length) -ForegroundColor DarkGray
}

function Ask {
    param($Prompt, $Default = "")
    if ($Default -ne "") {
        $r = Read-Host "$Prompt [$Default]"
        if ([string]::IsNullOrWhiteSpace($r)) { return $Default }
        return $r
    }
    return (Read-Host $Prompt)
}

function Confirm-Action {
    param($Prompt)
    $r = Read-Host "$Prompt [y/N]"
    return ($r -match '^[Yy]$')
}

function Ask-Port {
    param($Prompt, $Default)
    while ($true) {
        $v = Ask $Prompt $Default
        $n = 0
        if ([int]::TryParse($v, [ref]$n)) {
            if ($n -ge 1 -and $n -le 65535) { return $n }
        }
        Write-Warn "Not a valid port (1-65535): $v"
    }
}

function Test-Command { param($n) return [bool](Get-Command $n -ErrorAction SilentlyContinue) }

# ---------------------------------------------------------------------------
# state
# ---------------------------------------------------------------------------

function Save-State {
    param($InstallDir, $ModelDir)
    $dir = Split-Path -Parent $StateFile
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Force $dir | Out-Null }
    @{ InstallDir = $InstallDir; ModelDir = $ModelDir } | ConvertTo-Json | Out-File $StateFile -Encoding utf8
}

function Get-State {
    if (Test-Path $StateFile) {
        try { return (Get-Content $StateFile -Raw | ConvertFrom-Json) } catch { return $null }
    }
    return $null
}

function Require-Install {
    $s = Get-State
    if ($s -and $s.InstallDir -and (Test-Path (Join-Path $s.InstallDir "go.mod"))) { return $s }
    Write-Warn "No installation recorded."
    $d = Ask "Path to the existing installation" $DefaultInstallDir
    if (-not (Test-Path (Join-Path $d "go.mod"))) { Fail "Not an installation directory: $d" }
    $md = $DefaultModelDir
    if ($s -and $s.ModelDir) { $md = $s.ModelDir }
    return [pscustomobject]@{ InstallDir = $d; ModelDir = $md }
}

# ---------------------------------------------------------------------------
# swarm key
# ---------------------------------------------------------------------------

# libp2p PSK: two header lines plus 32 random bytes as hex, LF endings, 96 bytes.
# WriteAllText is deliberate -- Set-Content and ">" add a BOM and/or CRLF, which the
# parser rejects, and the resulting error does not point at line endings.
function New-SwarmKey {
    param($Path)
    $bytes = New-Object byte[] 32
    [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
    $hex = -join ($bytes | ForEach-Object { $_.ToString("x2") })
    [IO.File]::WriteAllText($Path, "/key/swarm/psk/1.0.0/`n/base16/`n$hex`n")
}

function Test-SwarmKey {
    param($Path)
    if (-not (Test-Path $Path)) { return $false }
    $lines = [IO.File]::ReadAllText($Path) -split "`n"
    if ($lines.Count -lt 3) { return $false }
    if ($lines[0].Trim() -ne "/key/swarm/psk/1.0.0/") { return $false }
    if ($lines[2].Trim() -notmatch '^[0-9a-fA-F]{64}$') { return $false }
    return $true
}

function Setup-SwarmKey {
    param($InstallDir)
    $dest = Join-Path $InstallDir "swarm.key"

    if ((Test-Path $dest) -and (Test-SwarmKey $dest)) {
        Write-Ok "Existing swarm.key kept."
        return
    }

    Write-Host ""
    Write-Host "The swarm key (PSK) decides which private network this node joins."
    Write-Host "  - Joining an existing swarm: give the path to that swarm's key (must match exactly)."
    Write-Host "  - Starting a new swarm:      leave blank and one will be generated."
    Write-Host ""
    $src = Ask "Path to an existing swarm.key (blank = generate new)" ""

    if ($src -ne "") {
        if (-not (Test-Path $src)) { Fail "No such file: $src" }
        if (-not (Test-SwarmKey $src)) { Fail "Not a valid libp2p swarm.key: $src" }
        Copy-Item $src $dest -Force
        Write-Ok "swarm.key installed from $src"
    } else {
        New-SwarmKey $dest
        Write-Ok "New swarm.key generated."
        Write-Warn "Every node in this swarm needs this exact file. Back it up; it cannot be recovered."
    }
    Write-Host ("  sha256: " + (Get-FileHash $dest -Algorithm SHA256).Hash.ToLower())
}

# ---------------------------------------------------------------------------
# prerequisites
# ---------------------------------------------------------------------------

function Check-Prereqs {
    param($RelayOnly)
    $missing = @()
    foreach ($c in @("git", "go", "npm")) {
        if (-not (Test-Command $c)) { $missing += $c }
    }
    if (-not $RelayOnly -and -not (Test-Command "uv")) { $missing += "uv" }

    if ($missing.Count -gt 0) {
        Write-Err "Missing required commands: $($missing -join ', ')"
        Write-Host "  git  : https://git-scm.com/"
        Write-Host "  go   : https://go.dev/dl/  (1.26+)"
        Write-Host "  npm  : https://nodejs.org/ (22+, needed to build the dashboard)"
        Write-Host "  uv   : powershell -ExecutionPolicy ByPass -c `"irm https://astral.sh/uv/install.ps1 | iex`""
        return $false
    }

    if (Test-Command "nvidia-smi") {
        $gpu = (nvidia-smi --query-gpu=name,driver_version --format=csv,noheader 2>$null | Select-Object -First 1)
        if ($gpu) { Write-Ok "GPU: $gpu" }
    } elseif (-not $RelayOnly) {
        Write-Warn "nvidia-smi not found. Without a GPU, choose relay-only mode instead."
    }
    return $true
}

# ---------------------------------------------------------------------------
# python / vLLM environment
# ---------------------------------------------------------------------------

function Get-VenvPython {
    param($InstallDir)
    $p = Join-Path $InstallDir "vllm-windows\.venv\Scripts\python.exe"
    if (Test-Path $p) { return $p }
    return $null
}

# vLLM 0.9.2's transformers_utils/configs/ovis.py calls AutoConfig.register("aimv2", ...)
# (and two visual-tokenizer variants) at import time. Transformers ships its own aimv2
# config -- confirmed present in 4.57.6 and in 5.x -- so that call raises
#   ValueError: 'aimv2' is already used by a Transformers config, pick another name.
# and vLLM cannot be imported at all. Pinning transformers does NOT avoid this: it is
# present across the whole 4.51-4.x range this project supports.
#
# Wrap each bare registration in try/except so an already-registered config is a no-op.
# Line-based rather than regex: a -replace with a single-quoted replacement would insert
# literal `n characters instead of newlines and silently corrupt the file. Re-running is
# safe -- once patched the calls are indented, so they no longer match the anchor.
function Repair-VllmOvisConfig {
    param($InstallDir)
    $ovis = Join-Path $InstallDir "vllm-windows\.venv\Lib\site-packages\vllm\transformers_utils\configs\ovis.py"
    if (-not (Test-Path $ovis)) { return }

    $out = New-Object System.Collections.Generic.List[string]
    $patched = 0
    foreach ($line in [IO.File]::ReadAllLines($ovis)) {
        if ($line -match '^AutoConfig\.register\(') {
            $out.Add('try:')
            $out.Add('    ' + $line)
            $out.Add('except ValueError:')
            $out.Add('    pass')
            $patched++
        } else {
            $out.Add($line)
        }
    }
    if ($patched -gt 0) {
        [IO.File]::WriteAllLines($ovis, $out)
        Write-Ok "Patched vLLM ovis.py ($patched config registrations) to tolerate transformers' own aimv2."
    }
}

function Setup-VllmEnv {
    param($InstallDir)

    $venvRoot = Join-Path $InstallDir "vllm-windows"
    $py = Get-VenvPython $InstallDir
    if ($py) {
        Write-Ok "Python environment already present."
        if (-not (Confirm-Action "Rebuild it from scratch?")) { return $py }
        Remove-Item (Join-Path $venvRoot ".venv") -Recurse -Force -ErrorAction SilentlyContinue
    }

    if (-not (Test-Path $venvRoot)) { New-Item -ItemType Directory -Force $venvRoot | Out-Null }

    # Offer newer wheels, but default to the pair this project actually verified.
    $tag = $VerifiedVllmTag; $wheel = $VerifiedVllmWheel
    Write-Host ""
    Write-Host "vLLM build for Windows (from SystemPanic/vllm-windows):"
    Write-Host "  Verified by this project: $VerifiedVllmTag ($VerifiedVllmWheel, CUDA 12.4)"
    Write-Host "  Newer releases exist but target CUDA 13.x and need a matching driver and"
    Write-Host "  torch build. They are not validated here."
    if (Confirm-Action "Use a different release tag?") {
        $tag = Ask "Release tag (e.g. v0.26.0)" $VerifiedVllmTag
        $wheel = Ask "Wheel filename for that tag" $VerifiedVllmWheel
        Write-Warn "Unverified combination: you may need to adjust the torch version by hand."
    }

    Write-Info "Creating Python 3.12 virtual environment"
    Push-Location $venvRoot
    try {
        & uv venv .venv --python 3.12
        if (-not $?) { Fail "uv venv failed." }

        Write-Info "Installing PyTorch (this downloads several GB)"
        & uv pip install --python .venv\Scripts\python.exe @VerifiedTorch --extra-index-url $VerifiedTorchIdx
        if (-not $?) { Fail "PyTorch install failed." }

        $encoded = $wheel -replace '\+', '%2B'
        $url = "https://github.com/SystemPanic/vllm-windows/releases/download/$tag/$encoded"
        $local = Join-Path $venvRoot $wheel
        Write-Info "Downloading $wheel"
        try { Invoke-WebRequest -Uri $url -OutFile $local -UseBasicParsing }
        catch { Fail "Could not download the wheel:`n  $url`n  $($_.Exception.Message)" }

        Write-Info "Installing vLLM"
        & uv pip install --python .venv\Scripts\python.exe $local
        if (-not $?) { Fail "vLLM install failed." }
        # vLLM 0.9.2 pulls in transformers but needs 4.x (5.x removes APIs it uses,
        # e.g. tokenizer.all_special_tokens_extended). Qwen3 needs >=4.51. Pin so both hold.
        & uv pip install --python .venv\Scripts\python.exe "transformers>=4.51.0,<5.0.0"
        Remove-Item $local -Force -ErrorAction SilentlyContinue
    } finally { Pop-Location }

    Repair-VllmOvisConfig $InstallDir

    $py = Get-VenvPython $InstallDir
    if (-not $py) { Fail "Environment setup finished but python.exe is missing." }

    Write-Info "Verifying"
    $check = & $py -c "import torch,vllm;print(torch.__version__,torch.cuda.is_available(),vllm.__version__)" 2>&1
    Write-Ok "torch/cuda/vllm: $check"
    return $py
}

# ---------------------------------------------------------------------------
# models
# ---------------------------------------------------------------------------

function Get-ModelDirName { param($Repo) return ($Repo -split '/')[-1] }

function Show-Models {
    param($ModelDir, $InstallDir)
    if (-not (Test-Path $ModelDir)) { Write-Host "  (no models in $ModelDir)"; return @() }
    $dirs = @(Get-ChildItem $ModelDir -Directory -ErrorAction SilentlyContinue)
    if ($dirs.Count -eq 0) { Write-Host "  (no models in $ModelDir)"; return @() }

    $current = ""
    $envFile = Join-Path $InstallDir ".env"
    if (Test-Path $envFile) {
        $line = Get-Content $envFile | Where-Object { $_ -match '^ABS_MODEL_PATH=' } | Select-Object -First 1
        if ($line) { $current = ($line -split '=', 2)[1] }
    }

    for ($i = 0; $i -lt $dirs.Count; $i++) {
        $size = "{0:N1} GB" -f ((Get-ChildItem $dirs[$i].FullName -Recurse -File -ErrorAction SilentlyContinue |
                 Measure-Object Length -Sum).Sum / 1GB)
        $mark = "  "
        if ($dirs[$i].FullName -eq $current) { $mark = "->" }
        Write-Host ("   {0} {1,2}) {2,-40} {3}" -f $mark, ($i + 1), $dirs[$i].Name, $size)
    }
    return $dirs
}

function Select-Model {
    param($Dirs)
    if ($Dirs.Count -eq 0) { return $null }
    $n = Ask "Number" ""
    $idx = 0
    if (-not [int]::TryParse($n, [ref]$idx)) { return $null }
    if ($idx -lt 1 -or $idx -gt $Dirs.Count) { return $null }
    return $Dirs[$idx - 1].FullName
}

function Download-Model {
    param($ModelDir, $InstallDir, $Repo = "")

    if (-not (Test-Path $ModelDir)) { New-Item -ItemType Directory -Force $ModelDir | Out-Null }

    if ($Repo -eq "") {
        Write-Host ""
        Write-Host "Enter any Hugging Face repo id, for example:"
        Write-Host "    Qwen/Qwen3-4B-AWQ            Qwen/Qwen2.5-7B-Instruct-AWQ"
        Write-Host "    meta-llama/Llama-3.1-8B      mistralai/Mistral-7B-Instruct-v0.3"
        Write-Host ""
        $Repo = Ask "Model repo id" $DefaultModel
    }
    if ($Repo -notmatch '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$') {
        Write-Err "That does not look like a repo id (expected 'owner/name'): $Repo"
        return $null
    }

    $dest = Join-Path $ModelDir (Get-ModelDirName $Repo)
    if ((Test-Path $dest) -and (Get-ChildItem $dest -ErrorAction SilentlyContinue)) {
        Write-Warn "Already present: $dest"
        if (-not (Confirm-Action "Re-download (existing directory will be replaced)?")) { return $dest }
        Remove-Item $dest -Recurse -Force
    }

    $py = Get-VenvPython $InstallDir
    Write-Info "Downloading $Repo -> $dest"
    if ($py) {
        # huggingface_hub ships as a vLLM dependency, so the venv can fetch weights
        # without git-lfs and without the duplicate .git copy a clone leaves behind.
        & $py -c "from huggingface_hub import snapshot_download; snapshot_download('$Repo', local_dir=r'$dest')"
        if (-not $?) { Remove-Item $dest -Recurse -Force -ErrorAction SilentlyContinue; Fail "Download failed." }
    } else {
        if (-not (Test-Command "git")) { Fail "Need either the Python environment or git to download models." }
        & git lfs install --skip-repo 2>$null | Out-Null
        & git clone "https://huggingface.co/$Repo" $dest
        if (-not $?) { Remove-Item $dest -Recurse -Force -ErrorAction SilentlyContinue; Fail "Clone failed." }
    }
    Write-Ok "Model ready: $dest"
    return $dest
}

# Point .env and config.json at a model. Both must agree, so always write both.
function Set-ActiveModel {
    param($InstallDir, $ModelPath)
    $name = Split-Path -Leaf $ModelPath

    $envFile = Join-Path $InstallDir ".env"
    if (Test-Path $envFile) {
        $c = Get-Content $envFile | ForEach-Object { $_ -replace '^ABS_MODEL_PATH=.*', "ABS_MODEL_PATH=$ModelPath" }
        [IO.File]::WriteAllText($envFile, ($c -join "`n") + "`n")
    }
    $cfgFile = Join-Path $InstallDir "config.json"
    if (Test-Path $cfgFile) {
        $raw = [IO.File]::ReadAllText($cfgFile)
        $raw = $raw -replace '"model_name"\s*:\s*"[^"]*"', "`"model_name`": `"$name`""
        $esc = $ModelPath -replace '\\', '\\'
        $raw = $raw -replace '"model_path"\s*:\s*"[^"]*"', "`"model_path`": `"$esc`""
        [IO.File]::WriteAllText($cfgFile, $raw)
    }
    Write-Ok "Active model set to $name"
}

function Models-Menu {
    while ($true) {
        $s = Require-Install
        Write-Heading "Models"
        $dirs = Show-Models $s.ModelDir $s.InstallDir
        Write-Host ""
        Write-Host "  1) Download a model from Hugging Face"
        Write-Host "  2) Switch active model"
        Write-Host "  3) Delete a model"
        Write-Host "  4) Back"
        switch (Ask "Choice" "4") {
            "1" { Download-Model $s.ModelDir $s.InstallDir | Out-Null }
            "2" {
                $d = Select-Model $dirs
                if ($d) { Set-ActiveModel $s.InstallDir $d } else { Write-Warn "Cancelled." }
            }
            "3" {
                $d = Select-Model $dirs
                if ($d) {
                    # Warn when deleting the model the node is configured to load, since
                    # it will not start again until another one is selected. install.sh
                    # does the same.
                    $envFile = Join-Path $s.InstallDir ".env"
                    if (Test-Path $envFile) {
                        $cur = Get-Content $envFile | Where-Object { $_ -match '^ABS_MODEL_PATH=' } | Select-Object -First 1
                        if ($cur -and (($cur -split '=', 2)[1] -eq $d)) {
                            Write-Warn "That is the model currently in use. The node will not start until another is selected."
                        }
                    }
                    Write-Host "About to delete: $d"
                    if (Confirm-Action "Delete permanently?") {
                        Remove-Item $d -Recurse -Force; Write-Ok "Deleted."
                    }
                } else { Write-Warn "Cancelled." }
            }
            default { return }
        }
    }
}

# ---------------------------------------------------------------------------
# install / uninstall
# ---------------------------------------------------------------------------

function Write-NodeConfig {
    param($InstallDir, $Cfg)
    [IO.File]::WriteAllText((Join-Path $InstallDir ".env"), (@"
# Generated by install.ps1 on $(Get-Date -Format o)
ABS_MODEL_PATH=$($Cfg.ModelPath)
SERVER_ADDRESS=$($Cfg.Bootstrap)
IFACE=$($Cfg.Iface)
CLIENT_WEB_PORT=$($Cfg.WebPort)
"@ -replace "`r`n", "`n"))

    $modelPathJson = $Cfg.ModelPath -replace '\\', '\\'
    [IO.File]::WriteAllText((Join-Path $InstallDir "config.json"), (@"
{
  "version": "1.0",
  "web_port": $($Cfg.WebPort),
  "proxy_port": $($Cfg.ProxyPort),
  "p2p": {
    "server_address": "$($Cfg.Bootstrap)",
    "server_addresses": []
  },
  "docker": {
    "container_name": "vllm_node",
    "image": "vllm-runtime-mooncake:latest",
    "network": "host",
    "shm_size": "16gb",
    "iface": "$($Cfg.Iface)"
  },
  "paths": {
    "config_path": "./config.json",
    "model_path": "$modelPathJson",
    "mooncake_path": "./mooncake.json"
  },
  "vllm": {
    "model_name": "$($Cfg.ModelName)",
    "max_model_len": 8192,
    "gpu_memory_utilization": $($Cfg.GpuUtil),
    "port": $($Cfg.VllmPort),
    "tensor_parallel_size": 1,
    "dtype": "float16",
    "kv_role": "kv_both",
    "mooncake_bootstrap_port": $($Cfg.MooncakePort),
    "mooncake_abort_request_timeout": 15,
    "attention_backend": "FLASH_ATTN",
    "placement_group_bundle_strategy": "SPREAD"
  },
  "server_mode": {
    "enabled": $($Cfg.HubEnabled),
    "relay_only": $($Cfg.RelayOnly),
    "p2p_port": $($Cfg.HubP2PPort),
    "proxy_port": $($Cfg.HubProxyPort),
    "database_path": "./peers.db",
    "max_fail_count": 3,
    "check_interval_sec": 30,
    "cluster": {
      "prefill_nodes": 0,
      "decode_nodes": 0
    }
  }
}
"@ -replace "`r`n", "`n"))
}

function Do-Install {
    Write-Heading "Install Mooncake 2.0 Client Agent (Windows native)"

    Write-Host ""
    Write-Host "How should this node contribute?"
    Write-Host "  1) Inference node  - runs a local GPU engine (needs an NVIDIA GPU and ~15 GB of downloads)"
    Write-Host "  2) Relay only      - contributes network relaying, no GPU, no Python environment"
    $relayOnly = ((Ask "Choice" "1") -eq "2")

    if (-not (Check-Prereqs $relayOnly)) { return }

    $s = Get-State
    $defDir = $DefaultInstallDir
    if ($s -and $s.InstallDir) { $defDir = $s.InstallDir }
    $installDir = Ask "Install directory" $defDir

    if ((Test-Path $installDir) -and (Get-ChildItem $installDir -ErrorAction SilentlyContinue)) {
        if (Test-Path (Join-Path $installDir "go.mod")) {
            Write-Warn "An installation already exists at $installDir"
            if (-not (Confirm-Action "Update it in place (config and swarm.key are preserved)?")) { return }
            Push-Location $installDir
            try { & git pull --ff-only } catch { Write-Warn "git pull failed; continuing with the existing checkout." }
            Pop-Location
        } else {
            Fail "$installDir exists, is not empty, and is not an installation. Choose another path."
        }
    } else {
        Write-Info "Cloning $RepoUrl"
        & git clone --depth 1 $RepoUrl $installDir
        if (-not $?) { Fail "Clone failed." }
    }

    $hubEnabled = $relayOnly
    if (-not $relayOnly) {
        $hubEnabled = Confirm-Action "Also run hub services (peer database, scoring, topology API)?"
    }

    Setup-SwarmKey $installDir

    Write-Host ""
    $bootstrap = Ask "Bootstrap peer multiaddress (blank = start a new swarm)" $DefaultBootstrap
    $iface = Ask "Network interface name (for NCCL/GLOO)" "eth0"

    Write-Host ""
    $ports = @{
        WebPort = $DefaultWebPort; ProxyPort = $DefaultProxyPort; VllmPort = $DefaultVllmPort
        MooncakePort = $DefaultMooncakePort; HubP2PPort = $DefaultHubP2PPort; HubProxyPort = $DefaultHubProxyPort
    }
    if (-not (Confirm-Action "Use default ports (web $DefaultWebPort, gateway $DefaultProxyPort, vLLM $DefaultVllmPort)?")) {
        $ports.WebPort      = Ask-Port "Web dashboard port" $DefaultWebPort
        $ports.ProxyPort    = Ask-Port "OpenAI gateway port" $DefaultProxyPort
        $ports.VllmPort     = Ask-Port "vLLM engine port" $DefaultVllmPort
        $ports.MooncakePort = Ask-Port "Mooncake KV bootstrap port" $DefaultMooncakePort
        $ports.HubP2PPort   = Ask-Port "libp2p listen port" $DefaultHubP2PPort
        $ports.HubProxyPort = Ask-Port "Hub dispatcher port" $DefaultHubProxyPort
    }

    # Build the dashboard first: web.go embeds web-ui/dist, so it must exist before go build.
    Write-Info "Building the dashboard (npm)"
    Push-Location (Join-Path $installDir "web-ui")
    try {
        & npm ci
        if (-not $?) { Fail "npm ci failed." }
        & npm run build
        if (-not $?) { Fail "npm run build failed." }
    } finally { Pop-Location }

    Write-Info "Building the agent (go)"
    Push-Location $installDir
    try {
        & go build -o client.exe .
        if (-not $?) { Fail "go build failed." }
    } finally { Pop-Location }
    Write-Ok "client.exe built."

    $modelDir = $DefaultModelDir
    if ($s -and $s.ModelDir) { $modelDir = $s.ModelDir }
    $modelPath = ""
    $modelName = "relay"
    $gpuUtil = "0.75"

    if (-not $relayOnly) {
        Setup-VllmEnv $installDir | Out-Null

        Write-Host ""
        $modelDir = Ask "Model storage directory" $modelDir
        $repo = Ask "Hugging Face model to use" $DefaultModel
        $gpuUtil = Ask "GPU memory utilization (fraction of TOTAL VRAM)" "0.75"
        $modelPath = Download-Model $modelDir $installDir $repo
        if (-not $modelPath) { Fail "No model was installed." }
        $modelName = Split-Path -Leaf $modelPath
    }

    Write-NodeConfig $installDir ([pscustomobject]@{
        ModelPath = $modelPath; Bootstrap = $bootstrap; Iface = $iface
        WebPort = $ports.WebPort; ProxyPort = $ports.ProxyPort; VllmPort = $ports.VllmPort
        MooncakePort = $ports.MooncakePort; HubP2PPort = $ports.HubP2PPort; HubProxyPort = $ports.HubProxyPort
        ModelName = $modelName; GpuUtil = $gpuUtil
        HubEnabled = $hubEnabled.ToString().ToLower(); RelayOnly = $relayOnly.ToString().ToLower()
    })
    Save-State $installDir $modelDir

    Write-Heading "Installed"
    Write-Host "  Directory : $installDir"
    if (-not $relayOnly) { Write-Host "  Models    : $modelDir" }
    Write-Host "  Dashboard : http://localhost:$($ports.WebPort)"
    Write-Host "  Gateway   : http://localhost:$($ports.ProxyPort)/v1/chat/completions"
    if ($relayOnly) { Write-Host "  Role      : relay-only (no local inference)" }
    Write-Host ""
    Write-Host "  Start  : cd `"$installDir`"; .\client.exe"
    Write-Host "  Manage : powershell -ExecutionPolicy Bypass -File install.ps1"
    Write-Host ""

    if (Confirm-Action "Start the node now?") {
        Start-Process -FilePath (Join-Path $installDir "client.exe") -WorkingDirectory $installDir
        Write-Ok "Started. Model load takes 1-2 minutes before the gateway answers."
    }
}

function Do-Uninstall {
    Write-Heading "Uninstall"
    $s = Require-Install

    Write-Host "This will remove the installation at:"
    Write-Host "    $($s.InstallDir)"
    Write-Host ""
    if (-not (Confirm-Action "Continue?")) { Write-Info "Cancelled."; return }

    Get-Process -Name "client" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -like (Join-Path $s.InstallDir "*") } |
        ForEach-Object { Write-Info "Stopping client.exe (PID $($_.Id))"; Stop-Process -Id $_.Id -Force }
    Get-Process -Name "python", "pythonw" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -like (Join-Path $s.InstallDir "*") } |
        ForEach-Object { Write-Info "Stopping vLLM (PID $($_.Id))"; Stop-Process -Id $_.Id -Force }
    Start-Sleep -Seconds 3

    # swarm.key is unrecoverable and shared by the whole swarm, so offer a backup first.
    $key = Join-Path $s.InstallDir "swarm.key"
    if (Test-Path $key) {
        if (Confirm-Action "Back up swarm.key before deleting?") {
            $backup = Join-Path $HOME ("swarm.key.backup-" + (Get-Date -Format "yyyyMMdd-HHmmss"))
            Copy-Item $key $backup -Force
            Write-Ok "Saved to $backup"
        } else {
            Write-Warn "swarm.key will be destroyed. Nodes sharing it cannot be rejoined without it."
        }
    }

    # Killing client.exe does not guarantee Windows has released its handle on the Badger
    # peerstore's memory-mapped .vlog file yet -- Remove-Item can fail with "being used by
    # another process" for a few seconds afterward even though the process is already gone.
    # Retry with backoff instead of failing the whole uninstall on that race.
    $removed = $false
    for ($i = 0; $i -lt 5; $i++) {
        try { Remove-Item $s.InstallDir -Recurse -Force -ErrorAction Stop; $removed = $true; break }
        catch { Start-Sleep -Seconds 2 }
    }
    if (-not $removed) { Fail "Could not remove $($s.InstallDir): a file is still locked. Close any program using it and re-run uninstall." }
    Write-Ok "Removed $($s.InstallDir)"

    if ($s.ModelDir -and (Test-Path $s.ModelDir) -and (Get-ChildItem $s.ModelDir -ErrorAction SilentlyContinue)) {
        Write-Host ""
        Write-Host "Models are stored separately at $($s.ModelDir)"
        if (Confirm-Action "Delete downloaded models too?") {
            Remove-Item $s.ModelDir -Recurse -Force; Write-Ok "Removed $($s.ModelDir)"
        } else { Write-Info "Kept $($s.ModelDir)" }
    }

    Remove-Item $StateFile -Force -ErrorAction SilentlyContinue
    Write-Host ""
    Write-Ok "Uninstalled."
}

function Do-Status {
    Write-Heading "Status"
    $s = Get-State
    if (-not $s -or -not $s.InstallDir -or -not (Test-Path (Join-Path $s.InstallDir "go.mod"))) {
        Write-Host "  Not installed."
        return
    }
    Write-Host "  Directory : $($s.InstallDir)"

    $envFile = Join-Path $s.InstallDir ".env"
    $webPort = $DefaultWebPort
    if (Test-Path $envFile) {
        foreach ($l in Get-Content $envFile) {
            if ($l -match '^ABS_MODEL_PATH=(.*)') {
                # Relay-only nodes have no model, so the value is empty. Say so rather
                # than printing a blank field that reads like something went wrong.
                $m = $Matches[1]
                if ([string]::IsNullOrWhiteSpace($m)) { $m = "(none - relay-only node)" }
                Write-Host "  Model     : $m"
            }
            if ($l -match '^CLIENT_WEB_PORT=(.*)') { $webPort = $Matches[1]; Write-Host "  Web port  : $webPort" }
        }
    }
    $key = Join-Path $s.InstallDir "swarm.key"
    if (Test-Path $key) {
        Write-Host ("  Key       : sha256 " + (Get-FileHash $key -Algorithm SHA256).Hash.ToLower().Substring(0, 16) + "...")
    }

    $proc = Get-Process -Name "client" -ErrorAction SilentlyContinue |
            Where-Object { $_.Path -like (Join-Path $s.InstallDir "*") }
    if ($proc) { Write-Host "  Process   : running (PID $($proc.Id))" -ForegroundColor Green }
    else { Write-Host "  Process   : stopped" -ForegroundColor Yellow }

    try {
        $r = Invoke-WebRequest -Uri "http://127.0.0.1:$webPort/api/node_info" -TimeoutSec 3 -UseBasicParsing
        if ($r.StatusCode -eq 200) { Write-Host "  Dashboard : responding on http://localhost:$webPort" -ForegroundColor Green }
    } catch { }
}

function Show-Usage {
    @"
Mooncake 2.0 Client Agent -- Windows installer and manager

  install.ps1              interactive menu
  install.ps1 install      install / update
  install.ps1 uninstall    remove this installation
  install.ps1 models       manage models
  install.ps1 status       show current state
  install.ps1 -Command help

Windows runs the agent natively (Go binary + local Python/vLLM), not under Docker.
Defaults: install $DefaultInstallDir, models $DefaultModelDir,
dashboard $DefaultWebPort, gateway $DefaultProxyPort. Every prompt offers these as
defaults, so pressing Enter accepts them.
"@ | Write-Host
}

function Main-Menu {
    while ($true) {
        Write-Heading "Mooncake 2.0 Client Agent (Windows)"
        $s = Get-State
        if ($s -and $s.InstallDir -and (Test-Path (Join-Path $s.InstallDir "go.mod"))) {
            Write-Host "  Installed at $($s.InstallDir)"
        } else { Write-Host "  Not installed." }
        Write-Host ""
        Write-Host "  1) Install / update"
        Write-Host "  2) Manage models (download / switch / delete)"
        Write-Host "  3) Status"
        Write-Host "  4) Uninstall"
        Write-Host "  5) Exit"
        switch (Ask "Choice" "5") {
            "1" { Do-Install }
            "2" { Models-Menu }
            "3" { Do-Status }
            "4" { Do-Uninstall }
            default { return }
        }
    }
}

switch ($Command.ToLower()) {
    "install"   { Do-Install }
    "uninstall" { Do-Uninstall }
    "models"    { Models-Menu }
    "status"    { Do-Status }
    "help"      { Show-Usage }
    "-h"        { Show-Usage }
    "--help"    { Show-Usage }
    ""          { Main-Menu }
    default     { Write-Err "Unknown command: $Command"; Write-Host ""; Show-Usage; exit 1 }
}
