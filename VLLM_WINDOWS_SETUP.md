# Windows 環境下使用 uv 架設 vLLM + Qwen AWQ 模型完整指南

本指南詳細記錄在 **Windows 10 / 11** 本機環境下，使用現代套件管理工具 **`uv`** 建立獨立虛擬環境（`.venv`），並成功編譯/安裝 **vLLM (Windows 版)** 搭配 **Qwen AWQ** 量化模型進行 GPU 加速推論與 OpenAI API 伺服器架設的完整步驟。

---

## 系統先決條件 (Prerequisites)

1. **作業系統**：Windows 10 / Windows 11 (x64)
2. **顯示卡 (GPU)**：NVIDIA 獨立顯卡（RTX 30 系列、40 系列或以上，顯存建議 $\ge$ 6GB）
3. **NVIDIA 驅動程式**：Version $\ge$ 550.xx（支援 CUDA 12.x / 12.4+）
4. **必備工具**：
   - [Git for Windows](https://git-scm.com/)
   - [uv (極速 Python 套件管理器)](https://docs.astral.sh/uv/)
     ```powershell
     # 若尚未安裝 uv，可在 PowerShell 中執行：
     powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
     ```

---

## 第一步：下載 vllm-windows 倉庫

開啟 PowerShell 並進入工作目錄，執行 Clone：

```powershell
# 進入專案工作區
cd "c:\Users\chich\Documents\vllm\gemini test 20260821"

# Clone 專用倉庫
git clone https://github.com/SystemPanic/vllm-windows
cd vllm-windows
```

---

## 第二步：使用 uv 建立 Python 3.12 虛擬環境

`uv` 會自動下載並管理乾淨的 CPython 3.12 直譯器：

```powershell
# 使用 Python 3.12 建立 .venv 虛擬環境
uv venv .venv --python 3.12

# 啟用虛擬環境
.\.venv\Scripts\activate
```

---

## 第三步：下載並安裝 Windows 專用 vLLM Wheel 與 PyTorch

由於官方 vLLM 預設主要支援 Linux，Windows 環境需使用社群專門針對 MSVC 與 Windows 核心編譯之 Wheel：

```powershell
# 1. 下載適用於 CUDA 12.4 的 vLLM 0.9.2 Windows 專用 Wheel
gh release download v0.9.2 -R SystemPanic/vllm-windows -D wheels_v092
# (若無 GitHub CLI，可手動至 https://github.com/SystemPanic/vllm-windows/releases/tag/v0.9.2 下載 .whl 檔案)

# 2. 安裝 CUDA 12.4 版本的 PyTorch 2.6.0
uv pip install torch==2.6.0+cu124 torchvision==0.21.0+cu124 torchaudio==2.6.0+cu124 --extra-index-url https://download.pytorch.org/whl/cu124

# 3. 安裝 vLLM Windows Wheel 及其依賴
uv pip install wheels_v092\vllm-0.9.2+cu124-cp312-cp312-win_amd64.whl

# 4. 確保 Transformers 版本相容 (避免 config 註冊衝突)
uv pip install "transformers>=4.48.0,<4.50.0"
```

---

## 第四步：Windows 平台關鍵參數設定說明

在 Windows 上運行 vLLM 時，需設定以下環境變數以規避平台特異性問題：

| 環境變數 | 建議值 | 作用說明 |
| :--- | :--- | :--- |
| `USE_LIBUV` | `0` | 停用 libuv，避免 PyTorch 報錯缺少 libuv 支援 |
| `VLLM_USE_V1` | `0` | 啟用 Windows 上最穩定的 V0 推論引擎架構 |
| `VLLM_CUDART_SO_PATH` | 指向 `.venv` 內的 `cudart64_12.dll` | 讓 vLLM 核心動態庫能正確找到 CUDA Runtime DLL |
| `CUDA_VISIBLE_DEVICES` | `0` | 指定使用第 0 號 NVIDIA 獨立顯卡 |

---

## 第五步：撰寫推論啟動腳本 (`run_vllm_windows.py`)

在專案目錄下建立 [`run_vllm_windows.py`](file:///c:/Users/chich/Documents/vllm/gemini%20test%2020260821/run_vllm_windows.py)，內容如下：

```python
import os
import sys
import tempfile
import argparse

# 1. 設置 Windows 控制台 UTF-8 編碼
sys.stdout.reconfigure(encoding='utf-8')
sys.stderr.reconfigure(encoding='utf-8')

# 2. 設置 Windows 關鍵環境變數
os.environ["USE_LIBUV"] = "0"
os.environ["VLLM_USE_V1"] = "0"
os.environ["CUDA_VISIBLE_DEVICES"] = "0"
os.environ["HF_HUB_DISABLE_SYMLINKS_WARNING"] = "1"

# 3. 自動偵測並綁定 PyTorch 自帶的 cudart64_12.dll
venv_dir = os.path.dirname(os.path.dirname(sys.executable))
cudart_path = os.path.join(venv_dir, "Lib", "site-packages", "torch", "lib", "cudart64_12.dll")
if os.path.exists(cudart_path):
    os.environ["VLLM_CUDART_SO_PATH"] = cudart_path

# 4. 確保 site-packages 優先載入
for p in list(sys.path):
    if p.endswith("vllm-windows") or os.path.exists(os.path.join(p, "CMakeLists.txt")):
        sys.path.remove(p)

import torch
import torch.distributed as dist

# 5. 預先使用 FileStore 初始化 Gloo 分散式環境 (規避 Windows PyTorch TCPStore 格式化 Bug)
store_file = os.path.join(tempfile.gettempdir(), "vllm_dist_store.tmp")
if os.path.exists(store_file):
    try:
        os.remove(store_file)
    except Exception:
        pass

store = dist.FileStore(store_file, 1)
dist.init_process_group(
    backend="gloo",
    store=store,
    rank=0,
    world_size=1
)

from vllm import LLM, SamplingParams

def run_inference(model_name="Qwen/Qwen2.5-3B-Instruct-AWQ", prompt_text="請簡要自我介紹。"):
    print(f"=== vLLM Windows 推論 ===")
    print(f"模型: {model_name}")
    print(f"GPU : {torch.cuda.get_device_name(0)}")

    # 初始化 vLLM 引擎 (8GB VRAM 建議 gpu_memory_utilization 設為 0.65~0.70)
    llm = LLM(
        model=model_name,
        quantization="awq",
        gpu_memory_utilization=0.65,
        max_model_len=2048,
        trust_remote_code=True,
        enforce_eager=True
    )

    sampling_params = SamplingParams(
        temperature=0.7,
        top_p=0.9,
        max_tokens=256
    )

    outputs = llm.generate([prompt_text], sampling_params)

    for output in outputs:
        print("\n【模型生成結果】:")
        print(output.outputs[0].text.strip())

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", type=str, default="Qwen/Qwen2.5-3B-Instruct-AWQ")
    parser.add_argument("--prompt", type=str, default="請用繁體中文介紹 vLLM 在 Windows 上搭配 AWQ 的優勢。")
    args = parser.parse_args()
    run_inference(args.model, args.prompt)
```

---

## 第六步：執行推論測試

在 PowerShell 中執行：

```powershell
.\vllm-windows\.venv\Scripts\python run_vllm_windows.py --model "Qwen/Qwen2.5-3B-Instruct-AWQ"
```

> **推論效能表現**：
> - 顯存佔用：約 **1.95 GB**（在 8GB 筆電顯卡上極為輕量）。
> - 生成速度：約 **27 ~ 28 tokens/sec**。

---

## 第七步：架設 OpenAI 相容 API 伺服器

已為您建立整合好的 API 入口腳本 [`serve_api.py`](file:///c:/Users/chich/Documents/vllm/gemini%20test%2020260821/serve_api.py)。

---

## 第八步：將 vLLM 改為 Windows「常駐服務 (Background Daemon)」

若希望 vLLM 在 Windows 背景無感常駐運行（**無需保持終端機視窗開啟**，關閉終端也不受影響，隨時可供第三方應用或 WebUI 調用），可以使用為您配置的背景服務管理腳本：

### 1. 一鍵背景常駐啟動
在 PowerShell 中執行：
```powershell
.\start_vllm.ps1
```
- 腳本會使用 `pythonw.exe` 配合隱藏視窗（Hidden Window）在背景啟動服務。
- 自動輪詢直到模型載入完成並開放 Port 8000。
- 啟動成功後，即可自由關閉 PowerShell 視窗，服務將持續在後台運行。

### 2. 檢查常駐狀態
隨時檢查後台服務是否存活、PID 及模型健康度：
```powershell
.\status_vllm.ps1
```
輸出範例：
```text
===================================================
  vLLM Windows Service Status
===================================================
[STATUS] : RUNNING (Port 8000 is open)
[PID]    : 7036
[MODEL]  : Qwen/Qwen2.5-3B-Instruct-AWQ
[API]    : http://localhost:8000/v1/chat/completions (Healthy)
```

### 3. 停止常駐服務
若要釋放 GPU 顯存或更換模型，隨時執行停止腳本：
```powershell
.\stop_vllm.ps1
```

---

## 第九步：設定 Windows 開機自動常駐啟動 (選用)

若希望電腦開機登入後自動常駐啟動 vLLM：
1. 按下鍵盤 `Win + R`，輸入 `shell:startup` 按 Enter（開啟 Windows 啟動資料夾）。
2. 在該資料夾內新增一個捷徑，目標設定為：
   ```cmd
   powershell.exe -ExecutionPolicy Bypass -WindowStyle Hidden -File "c:\Users\chich\Documents\vllm\gemini test 20260821\start_vllm.ps1"
   ```
即可實現每次登入 Windows 時全自動在背景常駐就緒！

