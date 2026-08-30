// Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Package main implements process management and execution of Ray and vLLM inference engine containers.
package main

import (
	"bufio"         // 帶緩衝區的掃描器 (bufio.Scanner 逐行讀取 Docker/vLLM 輸出)
	"context"       // 上下文機制 (用於傳遞取消訊號與 Process 超時控制)
	"fmt"           // 格式化字串與控制台訊息輸出
	"io"            // io.Pipe：合併 docker logs 的 stdout/stderr 而不經過 shell
	"os"            // 作業系統檔案狀態查詢 (os.Stat)
	"os/exec"       // 外部子程序呼叫介面 (等同於 C 的 system/exec 或 Python 的 subprocess)
	"path/filepath" // 路徑轉換 (filepath.Abs 轉換為絕對路徑)
	"runtime"       // 跨平台作業系統偵測 (runtime.GOOS)
	"strings"       // 字串去除空白與前綴處理
	"time"          // 時間延遲 (time.Sleep / time.After)
)

// Runner 負責管理 Ray 叢集與 vLLM 程序/容器的完整生命週期。
type Runner struct {
	app     *App      // 指向根容器 App 的指標，用於讀取 Config 與呼叫 TUI 日誌記錄
	rayCmd  *exec.Cmd // Direct 模式下 Ray 程序控制器
	vllmCmd *exec.Cmd // Direct 模式下 vLLM 程序控制器
}

// NewRunner 建構函式：建立 Runner 實例。
func NewRunner(app *App) *Runner {
	return &Runner{app: app}
}

// isDirectExecution 檢查是否處於容器內原生執行 (All-in-One) 模式
func (r *Runner) isDirectExecution() bool {
	if os.Getenv("ALL_IN_ONE") == "true" || os.Getenv("DIRECT_MODE") == "true" {
		return true
	}
	// 如果本機有 vLLM 執行檔且找不到 docker.sock，預設進入 Direct 原生進程模式
	_, vllmErr := os.Stat("/opt/dynamo/venv/bin/vllm")
	_, sockErr := os.Stat("/var/run/docker.sock")
	return vllmErr == nil && os.IsNotExist(sockErr)
}

// Start 對外啟動進入點。
func (r *Runner) Start(ctx context.Context) {
	if runtime.GOOS == "windows" {
		r.app.TUI.AddVLLMLog("[System] 偵測到 Windows 作業系統 (runtime.GOOS=windows)，切換至 Windows 原生推論模式...")
		r.startVLLMWindows(ctx)
		return
	}

	if r.isDirectExecution() {
		r.app.TUI.AddVLLMLog("[System] 偵測到容器內原生執行環境 (All-in-One 模式)，啟動 Go 原生進程管理器...")
		r.startVLLMDirectly(ctx)
	} else {
		r.app.TUI.AddVLLMLog("[System] 偵測到外部 Docker 環境，啟動 Docker CLI 容器管理器...")
		r.startVLLMContainer(ctx)
	}
}

// Stop 清理序列：當使用者按 Q 退出或收到關機訊號時，終止相關子程序或刪除 Docker 容器。
func (r *Runner) Stop() {
	// 用 killProcessTree 而非 Process.Kill()：Ray 與 vLLM 都會再開孫行程（vLLM 的
	// APIServer 與 EngineCore 是各自獨立的 PID），只砍直接子行程會留下佔著顯存的孤兒，
	// 累積幾次重啟後新的 vLLM 就會因顯存不足而啟動失敗。
	if r.vllmCmd != nil && r.vllmCmd.Process != nil {
		fmt.Println("Terminating vLLM process tree...")
		if err := killProcessTree(r.vllmCmd); err != nil {
			fmt.Printf("warning: could not fully terminate vLLM process tree: %v\n", err)
		}
	}
	if r.rayCmd != nil && r.rayCmd.Process != nil {
		fmt.Println("Terminating Ray process tree...")
		if err := killProcessTree(r.rayCmd); err != nil {
			fmt.Printf("warning: could not fully terminate Ray process tree: %v\n", err)
		}
	}
	if runtime.GOOS == "windows" {
		// Windows 無 Docker 清理階段；行程樹已於上方收掉，Job Object 會在 client.exe
		// 結束時（含被強制終止）由核心再保證一次。
		return
	}
	if r.app.Config != nil && !r.isDirectExecution() {
		fmt.Println("Cleaning up Docker container...")
		exec.Command("docker", "rm", "-f", r.app.Config.Docker.ContainerName).Run()
	}
}

// startVLLMContainer 內部啟動實作：
// 【邏輯步驟說明】
// 1. 路徑檢查與準備：使用 filepath.Abs 將 config_path, model_path, mooncake_path 轉為絕對路徑。
// 2. 檔案存在性預檢：利用 os.Stat 檢查 3 個必要檔案/目錄是否存在，若缺件則輸出錯誤至 TUI 並直接 return。
// 3. 清除舊容器：呼叫 `docker rm -f <containerName>` 確保無同名舊容器運行。
// 4. 啟動 Ray 背景容器：
//   - 使用 `docker run -d` 背景啟動包含 GPU/IPC/SHM 設定的 Ray Head 節點。
//   - 掛載 config.json, 模型目錄, mooncake.json 至容器內。
//   - 設定 NCCL_SOCKET_IFNAME 與 GLOO_SOCKET_IFNAME 網卡環境變數。
//
// 5. 監控 Docker 容器日誌：開啟 Goroutine 執行 `docker logs -f` 並將輸出即時傳給 TUI。
// 6. 等待容器就緒：使用 select 阻塞 5 秒確保 Ray Head 節點初始化。
// 7. 構造 `docker exec` 的 argv 參數（不經過 bash -lc 字串插值，見下方安全性註解）：
//   - 以 `-e` 傳遞 PLACEMENT_GROUP_BUNDLE_STRATEGY, VLLM_USE_V1, ATTENTION_BACKEND, MOONCAKE_CONFIG_PATH 等環境變數。
//   - 構造 `/opt/dynamo/venv/bin/vllm serve` 命令列，帶入 GPU 記憶體使用率、TP 卡數、MooncakeConnector 角色等參數。
//
// 8. 透過 `docker exec` 執行 vLLM 程序：
//   - 綁定 StdoutPipe 與 StderrPipe。
//   - 開啟兩個 Goroutines 分別即時掃描 標準輸出 (AddVLLMLog) 與 標準錯誤輸出 (紅字記錄)。
//   - 呼叫 execCmd.Wait() 阻塞監控程序生命週期。
func (r *Runner) startVLLMContainer(ctx context.Context) {
	cfg := r.app.Config
	containerName := cfg.Docker.ContainerName
	configPath, _ := filepath.Abs(cfg.Paths.ConfigPath)
	modelPath, _ := filepath.Abs(cfg.Paths.ModelPath)
	mooncakePath, _ := filepath.Abs(cfg.Paths.MooncakePath)
	iface := cfg.Docker.Iface
	image := cfg.Docker.Image

	// 步驟 1: 檢查本機路徑存在性
	r.app.TUI.AddVLLMLog("[System] 正在檢查本機路徑...") // 至 tui.go 記錄控制台日誌
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 找不到 config 檔案: %s", configPath))
		return
	}
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 找不到模型路徑: %s", modelPath))
		return
	}
	if _, err := os.Stat(mooncakePath); os.IsNotExist(err) {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 找不到 mooncake 設定檔: %s", mooncakePath))
		return
	}

	// 步驟 2: 強制刪除上次未關閉的舊容器
	r.app.TUI.AddVLLMLog(fmt.Sprintf("[System] 正在強制關閉並移除舊容器 (%s)...", containerName))
	exec.Command("docker", "rm", "-f", containerName).Run()
	time.Sleep(1 * time.Second) // 等待 1 秒使 Docker Daemon 鎖完全釋放

	// 步驟 3: 啟動 Ray 背景容器 (docker run -d)
	r.app.TUI.AddVLLMLog("[System] 正在啟動 Ray 背景節點...")
	runCmd := exec.CommandContext(ctx, "docker", "run", "-d", "--name", containerName,
		"--net="+cfg.Docker.Network, "--gpus", "all", "--ipc=host", "--shm-size="+cfg.Docker.ShmSize,
		"-v", fmt.Sprintf("%s:/config.json", configPath),
		"-v", fmt.Sprintf("%s:/data/model", modelPath),
		"-v", fmt.Sprintf("%s:/data/mooncake.json", mooncakePath),
		"-e", fmt.Sprintf("NCCL_SOCKET_IFNAME=%s", iface),
		"-e", fmt.Sprintf("GLOO_SOCKET_IFNAME=%s", iface),
		"--entrypoint", "/opt/dynamo/venv/bin/ray",
		image, "start", "--head", "--dashboard-host", "0.0.0.0", "--dashboard-port", "8275", "--port", "6389", "--disable-usage-stats", "--block")

	if err := runCmd.Run(); err != nil {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 啟動 Ray 失敗: %v", err))
		return
	}

	// 步驟 4: 背景 Goroutine 負責後續的容器日誌讀取與 vLLM 程序執行
	go func() {
		r.app.TUI.AddVLLMLog("[System] 等待容器就緒 (sleep 5)...")

		// 子 Goroutine: 讀取 `docker logs -f` 輸出並寫送至 TUI。
		// 直接以 argv 呼叫 docker（不經過 bash -c 字串插值），containerName 來自 config.json，
		// 若透過 shell 字串組合會有指令注入風險。
		go func() {
			logsCmd := exec.CommandContext(ctx, "docker", "logs", "-f", containerName)
			pr, pw := io.Pipe()
			logsCmd.Stdout = pw
			logsCmd.Stderr = pw
			if err := logsCmd.Start(); err == nil {
				go func() {
					logsCmd.Wait()
					pw.Close()
				}()
				scanner := bufio.NewScanner(pr)
				for scanner.Scan() {
					r.app.TUI.AddDockerLog(scanner.Text()) // 至 tui.go 記錄 Docker 容器日誌
				}
			}
		}()

		// 阻塞等待 5 秒或接收 Context 取消訊號
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}

		r.app.TUI.AddVLLMLog(fmt.Sprintf("[System] Ray 啟動成功，準備啟動 vLLM (%s, port %d)...", cfg.VLLM.KVRole, cfg.VLLM.Port))

		// 驗證 Mooncake Bootstrap Port
		bootstrapPort := cfg.VLLM.MooncakeBootstrapPort
		if bootstrapPort <= 0 || bootstrapPort == cfg.ProxyPort {
			bootstrapPort = 8998
		}

		// 步驟 5/6: 透過 docker exec 啟動 vLLM 服務。
		// 直接以 argv 傳遞環境變數 (-e) 與 vllm 參數給 docker exec，不經過 bash -lc 字串插值 ——
		// cfg.VLLM.* 這些欄位可透過 /api/config 由設定檔重寫，若組成 shell 字串再交給 bash 執行，
		// 內含 shell metacharacter（如 `; $() 反引號`) 的值就能造成指令注入；改用 argv 傳遞後，
		// 這些值無論內容為何都只會被當成單一字面字串參數，不會被任何 shell 解析。
		kvTransferConfig := fmt.Sprintf(`{"kv_connector":"MooncakeConnector","kv_role":"%s"}`, cfg.VLLM.KVRole)
		execCmd := exec.CommandContext(ctx, "docker", "exec",
			"-e", fmt.Sprintf("PLACEMENT_GROUP_BUNDLE_STRATEGY=%s", cfg.VLLM.PlacementGroupBundleStrategy),
			"-e", "VLLM_USE_V1=1",
			"-e", fmt.Sprintf("ATTENTION_BACKEND=%s", cfg.VLLM.AttentionBackend),
			"-e", "MOONCAKE_CONFIG_PATH=/data/mooncake.json",
			"-e", fmt.Sprintf("VLLM_MOONCAKE_BOOTSTRAP_PORT=%d", bootstrapPort),
			"-e", fmt.Sprintf("VLLM_MOONCAKE_ABORT_REQUEST_TIMEOUT=%d", cfg.VLLM.MooncakeAbortRequestTimeout),
			containerName,
			"/opt/dynamo/venv/bin/vllm", "serve", "/data/model",
			"--served-model-name", cfg.VLLM.ModelName,
			"--dtype", cfg.VLLM.Dtype,
			"--max-model-len", fmt.Sprintf("%d", cfg.VLLM.MaxModelLen),
			"--max-num-seqs", fmt.Sprintf("%d", cfg.VLLM.MaxNumSeqs),
			"--gpu-memory-utilization", fmt.Sprintf("%.2f", cfg.VLLM.GpuMemoryUtilization),
			"--port", fmt.Sprintf("%d", cfg.VLLM.Port),
			"--tensor-parallel-size", fmt.Sprintf("%d", cfg.VLLM.TensorParallelSize),
			"--kv-transfer-config", kvTransferConfig,
		)

		stdout, err := execCmd.StdoutPipe()
		if err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 無法綁定 Stdout: %v", err))
			return
		}
		stderr, err := execCmd.StderrPipe()
		if err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 無法綁定 Stderr: %v", err))
			return
		}

		if err := execCmd.Start(); err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 啟動 vLLM 失敗: %v", err))
			return
		}

		// Goroutine: 掃描 vLLM 的 Stdout 標準輸出
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				r.app.TUI.AddVLLMLog(scanner.Text()) // 至 tui.go 記錄 vLLM 控制台日誌
			}
		}()

		// Goroutine: 掃描 vLLM 的 Stderr 標準錯誤輸出並標註紅字
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				text := scanner.Text()
				if strings.TrimSpace(text) != "" {
					r.app.TUI.AddVLLMLog("[red]" + text + "[-]")
				}
			}
		}()

		// 阻塞等待 vLLM 程序終止
		if err := execCmd.Wait(); err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[System] vLLM 程序終止: %v", err))
		} else {
			r.app.TUI.AddVLLMLog("[System] vLLM 程序已結束。")
		}
	}()
}

// startVLLMDirectly 原生進程啟動實作 (All-in-One 容器模式)
// 由 Go 程式直接啟動 Ray 節點與 vLLM 推論服務，無需經由外部 Docker CLI。
func (r *Runner) startVLLMDirectly(ctx context.Context) {
	cfg := r.app.Config

	modelPath, _ := filepath.Abs(cfg.Paths.ModelPath)
	mooncakePath, _ := filepath.Abs(cfg.Paths.MooncakePath)

	// 如果本機模型路徑不存在但容器內預設模型路徑 /data/model 存在，自動採用容器路徑
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		if _, errContainer := os.Stat("/data/model"); errContainer == nil {
			modelPath = "/data/model"
		}
	}
	if _, err := os.Stat(mooncakePath); os.IsNotExist(err) {
		if _, errContainer := os.Stat("/data/mooncake.json"); errContainer == nil {
			mooncakePath = "/data/mooncake.json"
		}
	}

	// 搜尋 Ray 與 vLLM 執行檔路徑
	rayBinary := "/opt/dynamo/venv/bin/ray"
	if _, err := os.Stat(rayBinary); os.IsNotExist(err) {
		rayBinary = "ray"
	}
	vllmBinary := "/opt/dynamo/venv/bin/vllm"
	if _, err := os.Stat(vllmBinary); os.IsNotExist(err) {
		vllmBinary = "vllm"
	}

	// 步驟 1: 啟動 Ray Head 節點
	r.app.TUI.AddVLLMLog("[System] 正在啟動 Ray Head 節點 (Direct Mode)...")
	r.rayCmd = exec.CommandContext(ctx, rayBinary, "start", "--head",
		"--dashboard-host", "0.0.0.0", "--dashboard-port", "8275", "--port", "6389", "--disable-usage-stats", "--block")

	prepareProcessTree(r.rayCmd)

	if err := r.rayCmd.Start(); err != nil {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 啟動 Ray 失敗: %v", err))
		return
	}
	if err := superviseProcessTree(r.rayCmd); err != nil {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Warning] 無法納管 Ray 行程樹（結束時可能殘留孤兒行程）: %v", err))
	}

	// 背景 Goroutine 負責後續 vLLM 的啟動與日誌掃描
	go func() {
		r.app.TUI.AddVLLMLog("[System] 等待 Ray 初始化 (sleep 3)...")
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return
		}

		bootstrapPort := cfg.VLLM.MooncakeBootstrapPort
		if bootstrapPort <= 0 || bootstrapPort == cfg.ProxyPort {
			bootstrapPort = 8998
		}

		r.app.TUI.AddVLLMLog(fmt.Sprintf("[System] 正在啟動 vLLM 服務 (模型: %s, 角色: %s, 埠: %d)...", cfg.VLLM.ModelName, cfg.VLLM.KVRole, cfg.VLLM.Port))

		kvConfigJSON := fmt.Sprintf(`{"kv_connector":"MooncakeConnector","kv_role":"%s"}`, cfg.VLLM.KVRole)

		r.vllmCmd = exec.CommandContext(ctx, vllmBinary, "serve", modelPath,
			"--served-model-name", cfg.VLLM.ModelName,
			"--dtype", cfg.VLLM.Dtype,
			"--max-model-len", fmt.Sprintf("%d", cfg.VLLM.MaxModelLen),
			"--max-num-seqs", fmt.Sprintf("%d", cfg.VLLM.MaxNumSeqs),
			"--gpu-memory-utilization", fmt.Sprintf("%.2f", cfg.VLLM.GpuMemoryUtilization),
			"--port", fmt.Sprintf("%d", cfg.VLLM.Port),
			"--tensor-parallel-size", fmt.Sprintf("%d", cfg.VLLM.TensorParallelSize),
			"--kv-transfer-config", kvConfigJSON,
		)

		r.vllmCmd.Env = append(os.Environ(),
			fmt.Sprintf("PLACEMENT_GROUP_BUNDLE_STRATEGY=%s", cfg.VLLM.PlacementGroupBundleStrategy),
			fmt.Sprintf("ATTENTION_BACKEND=%s", cfg.VLLM.AttentionBackend),
			fmt.Sprintf("MOONCAKE_CONFIG_PATH=%s", mooncakePath),
			fmt.Sprintf("VLLM_MOONCAKE_BOOTSTRAP_PORT=%d", bootstrapPort),
			fmt.Sprintf("VLLM_MOONCAKE_ABORT_REQUEST_TIMEOUT=%d", cfg.VLLM.MooncakeAbortRequestTimeout),
		)

		stdout, err := r.vllmCmd.StdoutPipe()
		if err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 無法綁定 vLLM Stdout: %v", err))
			return
		}
		stderr, err := r.vllmCmd.StderrPipe()
		if err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 無法綁定 vLLM Stderr: %v", err))
			return
		}

		prepareProcessTree(r.vllmCmd)

		if err := r.vllmCmd.Start(); err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 啟動 vLLM 程序失敗: %v", err))
			return
		}
		if err := superviseProcessTree(r.vllmCmd); err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[Warning] 無法納管 vLLM 行程樹（結束時可能殘留孤兒行程）: %v", err))
		}

		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				r.app.TUI.AddVLLMLog(scanner.Text())
			}
		}()

		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				text := scanner.Text()
				if strings.TrimSpace(text) != "" {
					r.app.TUI.AddVLLMLog("[red]" + text + "[-]")
				}
			}
		}()

		if err := r.vllmCmd.Wait(); err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[System] vLLM 程序終止: %v", err))
		} else {
			r.app.TUI.AddVLLMLog("[System] vLLM 程序已結束。")
		}
	}()
}

// startVLLMWindows Windows 原生進程啟動實作：
// 【邏輯說明】
// 1. 自動透過 nvidia-smi 檢測 GPU 狀態與顯存資訊，並即時輸出至 TUI 日誌。
// 2. 自動搜尋本地 Python (.venv) 虛擬環境路徑。
// 3. 配置 Windows 專用環境變數 (USE_LIBUV=0, VLLM_USE_V1=0, VLLM_CUDART_SO_PATH, etc.)。
// 4. 以原生方式啟動 vLLM OpenAI API 服務 (預設 port 8100)，並將 stdout/stderr 即時串流至 TUI 控制台。
func (r *Runner) startVLLMWindows(ctx context.Context) {
	cfg := r.app.Config

	// 步驟 1: 檢測 Windows GPU 狀態
	r.app.TUI.AddVLLMLog("[System] 正在透過 nvidia-smi 檢測 Windows 本機 GPU 狀態...")
	var gpuSummary string
	var driverVer string
	if r.app.Sys != nil {
		gpuTele := r.app.Sys.GetGPUTelemetry()
		gpuSummary = gpuTele.Summary
		driverVer = gpuTele.DriverVersion
	}
	if gpuSummary != "" && gpuSummary != "No GPU Detected" {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[System] 偵測到 GPU 硬體: %s (驅動版本: %s)", gpuSummary, driverVer))
	} else {
		r.app.TUI.AddVLLMLog("[Warning] 未偵測到 NVIDIA GPU 或尚未安裝驅動程式，將嘗試以可用環境啟動...")
	}

	// 步驟 2: 搜尋 Python 虛擬環境執行檔 (.venv)
	venvCandidates := []string{
		filepath.Join(".", ".venv", "Scripts", "python.exe"),
		filepath.Join(".", "vllm-windows", ".venv", "Scripts", "python.exe"),
		filepath.Join("..", "vllm-windows", ".venv", "Scripts", "python.exe"),
		filepath.Join("..", ".venv", "Scripts", "python.exe"),
	}

	var pythonBin string
	var venvRoot string
	for _, candidate := range venvCandidates {
		if _, err := os.Stat(candidate); err == nil {
			pythonBin, _ = filepath.Abs(candidate)
			venvRoot = filepath.Dir(filepath.Dir(pythonBin))
			break
		}
	}

	if pythonBin == "" {
		pythonBin = "python"
		r.app.TUI.AddVLLMLog("[Warning] 未找到 .venv 目錄，將嘗試使用系統預設 python 指令...")
	} else {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[System] 成功掛載 Python 虛擬環境: %s", pythonBin))
	}

	// 步驟 3: 決定模型名稱或路徑
	modelName := cfg.VLLM.ModelName
	substitutedTo := ""

	// A usable local model_path wins outright, so resolve it first. "/data/model" is the
	// Linux container mount point and never exists on Windows, so it does not count.
	localModelPath := ""
	if cfg.Paths.ModelPath != "" && cfg.Paths.ModelPath != "/data/model" {
		if _, err := os.Stat(cfg.Paths.ModelPath); err == nil {
			localModelPath, _ = filepath.Abs(cfg.Paths.ModelPath)
		}
	}

	switch {
	case localModelPath != "":
		// Local weights are present: load them, and leave substitutedTo empty. Checking
		// this before the fallback matters -- otherwise a node configured with local
		// weights *and* the default model_name would log a substitution warning that
		// never happened and advertise an alias for a model it is not serving.
		modelName = localModelPath
	case modelName == "" || modelName == "Qwen3-4B-AWQ" || modelName == "Qwen/Qwen3-4B-AWQ" || modelName == "yuanyi-default":
		// These are the Linux/Docker-mode defaults and there are no local weights to fall
		// back on, so download straight from Hugging Face rather than failing to start.
		// Recording it lets --served-model-name below reflect what is actually loaded
		// instead of silently claiming to serve the originally configured model. This is
		// currently the SAME model as the project-wide default (Qwen/Qwen3-4B-AWQ) --
		// Windows-native compatibility for it is unverified as of this change (unlike the
		// Qwen2.5-3B-Instruct-AWQ fallback this replaced, which was specifically chosen
		// because it was Windows-verified).
		modelName = "Qwen/Qwen3-4B-AWQ"
		substitutedTo = modelName
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Warning] 設定檔指定的模型 \"%s\" 在 Windows 原生模式下不可用，已改用 %s 代替", cfg.VLLM.ModelName, modelName))
	}

	vllmPort := cfg.VLLM.Port
	if vllmPort <= 0 {
		vllmPort = 8100
	}

	gpuUtil := cfg.VLLM.GpuMemoryUtilization
	if gpuUtil <= 0 {
		gpuUtil = 0.65
	}

	maxModelLen := cfg.VLLM.MaxModelLen
	if maxModelLen <= 0 {
		maxModelLen = 2048
	}

	maxNumSeqs := cfg.VLLM.MaxNumSeqs
	if maxNumSeqs <= 0 {
		maxNumSeqs = 32
	}

	r.app.TUI.AddVLLMLog(fmt.Sprintf("[System] 正在啟動 vLLM (模型: %s, 埠號: %d, 顯存佔用率: %.2f)...", modelName, vllmPort, gpuUtil))

	// 步驟 4: 構建 Windows 執行指令與相容性環境變數
	// 檢查是否有現成的 serve_api.py，若無則以 python -m vllm 啟動
	serveScriptCandidates := []string{
		filepath.Join(".", "serve_api.py"),
		filepath.Join("..", "serve_api.py"),
		filepath.Join(".", "vllm-windows", "serve_api.py"),
	}

	var serveScript string
	for _, sc := range serveScriptCandidates {
		if _, err := os.Stat(sc); err == nil {
			serveScript, _ = filepath.Abs(sc)
			break
		}
	}

	// vLLM's --served-model-name accepts multiple aliases, so expose the engine under
	// BOTH names when a substitution happened (see 步驟 3 above):
	//   - the configured model_name, so the gateway/swarm routing that references
	//     config.json's model_name keeps resolving (callers ask for the configured
	//     name; without this alias every such request 404s);
	//   - the model actually loaded, so /v1/models reports the truth about what is
	//     really answering rather than only echoing back the requested name.
	servedNames := []string{}
	if cfg.VLLM.ModelName != "" {
		servedNames = append(servedNames, cfg.VLLM.ModelName)
	}
	if substitutedTo != "" && substitutedTo != cfg.VLLM.ModelName {
		servedNames = append(servedNames, substitutedTo)
	}
	if len(servedNames) == 0 {
		servedNames = []string{modelName}
	}

	vllmArgs := []string{"--model", modelName, "--served-model-name"}
	vllmArgs = append(vllmArgs, servedNames...)
	vllmArgs = append(vllmArgs,
		"--quantization", "awq",
		"--gpu-memory-utilization", fmt.Sprintf("%.2f", gpuUtil),
		"--max-model-len", fmt.Sprintf("%d", maxModelLen),
		"--max-num-seqs", fmt.Sprintf("%d", maxNumSeqs),
		"--port", fmt.Sprintf("%d", vllmPort),
		"--host", "0.0.0.0",
		"--trust-remote-code",
		"--enforce-eager",
	)

	var cmd *exec.Cmd
	if serveScript != "" {
		cmd = exec.CommandContext(ctx, pythonBin, append([]string{serveScript}, vllmArgs...)...)
	} else {
		cmd = exec.CommandContext(ctx, pythonBin, append([]string{"-u", "-m", "vllm.entrypoints.openai.api_server"}, vllmArgs...)...)
	}

	// 注入 Windows 關鍵相容環境變數
	envVars := append(os.Environ(),
		"USE_LIBUV=0",
		"VLLM_USE_V1=0",
		"CUDA_VISIBLE_DEVICES=0",
		"HF_HUB_DISABLE_SYMLINKS_WARNING=1",
		"VLLM_WORKER_MULTIPROC_METHOD=spawn",
	)

	// 自動定位 PyTorch 的 cudart64_12.dll
	if venvRoot != "" {
		cudartPath := filepath.Join(venvRoot, "Lib", "site-packages", "torch", "lib", "cudart64_12.dll")
		if _, err := os.Stat(cudartPath); err == nil {
			envVars = append(envVars, fmt.Sprintf("VLLM_CUDART_SO_PATH=%s", cudartPath))
		}
	}
	cmd.Env = envVars

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 無法綁定 vLLM Stdout: %v", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 無法綁定 vLLM Stderr: %v", err))
		return
	}

	r.vllmCmd = cmd

	prepareProcessTree(cmd)

	if err := cmd.Start(); err != nil {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 啟動 Windows vLLM 程序失敗: %v", err))
		return
	}
	if err := superviseProcessTree(cmd); err != nil {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Warning] 無法納管 vLLM 行程樹（結束時可能殘留孤兒行程）: %v", err))
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			r.app.TUI.AddVLLMLog(scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			text := scanner.Text()
			if strings.TrimSpace(text) != "" {
				r.app.TUI.AddVLLMLog(text)
			}
		}
	}()

	go func() {
		if err := cmd.Wait(); err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[System] Windows vLLM 程序終止: %v", err))
		} else {
			r.app.TUI.AddVLLMLog("[System] Windows vLLM 程序已正常退出。")
		}
	}()
}
