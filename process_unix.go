//go:build !windows

// process_unix.go -- 非 Windows 平台的子行程樹生命週期管理，對應 process_windows.go。
//
// 與 Windows 相同的問題：Ray 與 vLLM 會各自再開孫行程，而 (*os.Process).Kill() 只送訊號給
// 直接子行程，孫行程會殘留並繼續佔用顯存。
//
// Unix 這邊用行程群組（process group）處理：啟動前設定 Setpgid 讓子行程自成一個新的群組，
// 它之後開的所有後代預設會繼承同一個 pgid；終止時對 -pgid 送訊號即可一次收掉整組。
//
// 注意這裡不像 Windows 有 Job Object 那種由核心保證的「父行程死亡即連坐」機制。若 client
// 被 SIGKILL 強制終止，行程群組仍會留下——這是 POSIX 的先天限制。實務上 All-in-One 容器
// 模式由 Docker 負責回收整個 cgroup，所以主要缺口在原生 Linux 直接執行的情境。
package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// prepareProcessTree 在 cmd.Start() 之前呼叫，讓子行程自成一個行程群組。
func prepareProcessTree(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// superviseProcessTree 在 cmd.Start() 之後呼叫。Unix 這邊該做的事已經在 prepareProcessTree
// 完成，這裡保留同名函式只是為了讓 runner.go 兩個平台共用同一段流程。
func superviseProcessTree(cmd *exec.Cmd) error { return nil }

// killProcessTree 對整個行程群組送 SIGKILL，一次收掉子行程與其所有後代。
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// 取不到群組（例如行程已結束）時，退回只收直接子行程。
		return cmd.Process.Kill()
	}
	// 負號代表「整個行程群組」而非單一 PID。
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("kill process group %d: %w", pgid, err)
	}
	return nil
}
