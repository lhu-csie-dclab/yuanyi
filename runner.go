// Package main implements process management and execution of Ray and vLLM inference engine containers.
package main

import (
	"bufio"         // 帶緩衝區的掃描器 (bufio.Scanner 逐行讀取 Docker/vLLM 輸出)
	"context"       // 上下文機制 (用於傳遞取消訊號與 Process 超時控制)
	"fmt"           // 格式化字串與控制台訊息輸出
	"os"            // 作業系統檔案狀態查詢 (os.Stat)
	"os/exec"       // 外部子程序呼叫介面 (等同於 C 的 system/exec 或 Python 的 subprocess)
	"path/filepath" // 路徑轉換 (filepath.Abs 轉換為絕對路徑)
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
	if r.vllmCmd != nil && r.vllmCmd.Process != nil {
		fmt.Println("Terminating vLLM process...")
		r.vllmCmd.Process.Kill()
	}
	if r.rayCmd != nil && r.rayCmd.Process != nil {
		fmt.Println("Terminating Ray process...")
		r.rayCmd.Process.Kill()
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
// 7. 構造 vLLM 啟動 Shell 命令 (vllmCmdStr)：
//   - 匯出 PLACEMENT_GROUP_BUNDLE_STRATEGY, VLLM_USE_V1, ATTENTION_BACKEND, MOONCAKE_CONFIG_PATH 等環境變數。
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

		// 子 Goroutine: 讀取 `docker logs -f` 輸出並寫送至 TUI
		go func() {
			logsCmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("docker logs -f %s 2>&1", containerName))
			logsStdout, err := logsCmd.StdoutPipe()
			if err == nil {
				logsCmd.Start()
				scanner := bufio.NewScanner(logsStdout)
				for scanner.Scan() {
					r.app.TUI.AddDockerLog(scanner.Text()) // 至 tui.go 記錄 Docker 容器日誌
				}
				logsCmd.Wait()
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

		// 步驟 5: 構造傳遞給 docker exec 的多行 Bash 命令字串
		vllmCmdStr := fmt.Sprintf(`export PLACEMENT_GROUP_BUNDLE_STRATEGY=%s
export VLLM_USE_V1=1
export ATTENTION_BACKEND=%s
export MOONCAKE_CONFIG_PATH=/data/mooncake.json
export VLLM_MOONCAKE_BOOTSTRAP_PORT=%d
export VLLM_MOONCAKE_ABORT_REQUEST_TIMEOUT=%d
/opt/dynamo/venv/bin/vllm serve /data/model \
  --served-model-name %s --dtype %s --max-model-len %d \
  --gpu-memory-utilization %.2f --port %d \
  --tensor-parallel-size %d \
  --kv-transfer-config "{\"kv_connector\":\"MooncakeConnector\",\"kv_role\":\"%s\"}"`,
			cfg.VLLM.PlacementGroupBundleStrategy,
			cfg.VLLM.AttentionBackend,
			bootstrapPort,
			cfg.VLLM.MooncakeAbortRequestTimeout,
			cfg.VLLM.ModelName, cfg.VLLM.Dtype, cfg.VLLM.MaxModelLen,
			cfg.VLLM.GpuMemoryUtilization, cfg.VLLM.Port,
			cfg.VLLM.TensorParallelSize,
			cfg.VLLM.KVRole,
		)

		// 步驟 6: 透過 docker exec 啟動 vLLM 服務
		execCmd := exec.CommandContext(ctx, "docker", "exec", containerName, "bash", "-lc", vllmCmdStr)

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

	if err := r.rayCmd.Start(); err != nil {
		r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 啟動 Ray 失敗: %v", err))
		return
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

		if err := r.vllmCmd.Start(); err != nil {
			r.app.TUI.AddVLLMLog(fmt.Sprintf("[Error] 啟動 vLLM 程序失敗: %v", err))
			return
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
