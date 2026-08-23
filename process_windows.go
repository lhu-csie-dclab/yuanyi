//go:build windows

// process_windows.go -- Windows 專用的子行程樹生命週期管理。
//
// 【為什麼需要這個檔案】
// Runner 在原生模式下會啟動 Ray 與 vLLM 兩個 Python 行程，而它們自己還會再開孫行程
// （vLLM 的日誌可以看到 APIServer 與 EngineCore 是各自獨立的 PID）。Go 標準庫的
// (*os.Process).Kill() 在 Windows 上等同於對「單一 PID」呼叫 TerminateProcess，不會連帶
// 終止後代，於是每次收掉節點都會殘留一組佔著顯存的孤兒 python.exe。累積幾次重啟之後，
// 新的 vLLM 就會因為顯存被殘留行程吃光而啟動失敗（"Free memory on device cuda:0 ... is
// less than desired GPU memory utilization"），而使用者完全看不出兩者的關聯。
//
// 更麻煩的是，若 client.exe 本身是被強制終止的（工作管理員、Stop-Process -Force、崩潰），
// Runner.Stop() 根本沒有機會執行，任何寫在關閉流程裡的清理程式碼都救不了。
//
// 因此這裡採用 Windows Job Object：把子行程指派給一個帶有
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE 的 Job。只要該 Job 的最後一個 handle 關閉（行程正常
// 結束、被強制終止、或整個崩潰都算，因為 Windows 會在行程結束時關閉其所有 handle），核心
// 就保證連同整棵行程樹一起收掉。這是作業系統強制執行的，不依賴我方程式碼跑完清理流程。
package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobOnce sync.Once
	jobH    windows.Handle
	jobErr  error
)

// processJob 取得（必要時建立）本行程專屬的 Job Object。
//
// 這個 handle 刻意不關閉：它的存活期就是 client.exe 的存活期。KILL_ON_JOB_CLOSE 的語意是
// 「最後一個 handle 關閉時終止 Job 內所有行程」，而行程結束時 Windows 會自動關閉其所有
// handle——持有到最後正是我們要的行為，包含被強制終止的情況。
func processJob() (windows.Handle, error) {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			jobErr = fmt.Errorf("CreateJobObject: %w", err)
			return
		}

		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(
			h,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			windows.CloseHandle(h)
			jobErr = fmt.Errorf("SetInformationJobObject: %w", err)
			return
		}
		jobH = h
	})
	return jobH, jobErr
}

// prepareProcessTree 在 cmd.Start() 之前呼叫。Windows 這邊不需要預先設定，實際綁定發生在
// superviseProcessTree（Job Object 只能對已存在的行程指派）。
func prepareProcessTree(cmd *exec.Cmd) {}

// superviseProcessTree 在 cmd.Start() 成功之後呼叫，把子行程指派給本行程的 Job Object，
// 使其連同所有後代與 client.exe 共存亡。
//
// 回傳的 error 僅供記錄：指派失敗不該讓節點無法啟動，只是退回成「可能殘留孤兒行程」的舊行為。
func superviseProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	job, err := processJob()
	if err != nil {
		return err
	}

	// PROCESS_SET_QUOTA + PROCESS_TERMINATE 是 AssignProcessToJobObject 所需的最小權限。
	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("OpenProcess(pid=%d): %w", cmd.Process.Pid, err)
	}
	defer windows.CloseHandle(h)

	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		return fmt.Errorf("AssignProcessToJobObject(pid=%d): %w", cmd.Process.Pid, err)
	}
	return nil
}

// killProcessTree 主動終止整棵行程樹，供 Runner.Stop() 在正常關閉時使用。
//
// Job Object 已經涵蓋「client.exe 結束」的情境，但 Stop() 也可能在行程仍要繼續存活時被呼叫
// （例如重啟 Runner 而不結束整個程式），那時仍需要顯式收掉這棵樹。taskkill /T 會走訪子孫，
// /F 強制終止。
func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	out, err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).CombinedOutput()
	if err != nil {
		// taskkill 失敗（例如行程已自行結束）時，至少確保直接子行程被收掉。
		_ = cmd.Process.Kill()
		return fmt.Errorf("taskkill /T /F /PID %d: %w: %s", pid, err, out)
	}
	return nil
}
