# GRF

![GRF Logo](desktop/frontend/src/assets/grf-lockup.svg)

**GRF** 是一个本地桌面应用，把 **Grok 免费号注册** 与 **OpenAI / Anthropic 兼容的推理网关** 整合到同一个图形界面里。

一条命令式的工作流，在一个窗口内完成：配置 → 启动批量注册 → 实时观察流水线 → 新账号自动入库 → 立即可被本地 API 网关调度。无需命令行，无需手工导入。

- 🖥️ **桌面控制台**：Wails v3 + React 的原生窗口程序，全中文界面
- 🔁 **一站式流水线**：注册 → OAuth → CPA JSON → 自动写入网关账号库
- 🌐 **内置 API 网关**：账号池负载均衡，对外暴露 `chat/completions`、`responses`、`messages` 等端点
- 🔑 **密钥管理**：生成 `grf_...` 格式 API Key，凭据以 AES-256-GCM 落盘
- 🪟 **Windows 原生体验**：进程外 worker、隐藏窗口后台运行、无 Python 依赖

---

## 目录

- [工作原理](#工作原理)
- [功能总览](#功能总览)
- [系统要求](#系统要求)
- [快速开始（Windows）](#快速开始windows)
- [使用指南](#使用指南)
- [内置 API 网关](#内置-api-网关)
- [前置依赖](#前置依赖)
- [macOS / Linux 构建](#macos--linux-构建)
- [开发模式](#开发模式)
- [目录结构](#目录结构)
- [配置参考](#配置参考)
- [命令行版](#命令行版)
- [常见问题](#常见问题)
- [License](#license)

---

## 工作原理

GRF 有两个可执行入口共享同一套注册引擎（`internal/` 包）：

```text
            ┌─────────────────────────────┐
            │   internal/ 注册引擎（共享）  │
            │  clearance · turnstile ·     │
            │  email · oauth · pipeline    │
            └──────────┬─────────┬─────────┘
                 ┌─────┘         └─────┐
        ┌────────▼────────┐   ┌────────▼────────┐
        │  grf-desktop    │   │   grf (CLI)     │
        │  Wails GUI 窗口  │   │  命令行可执行文件 │
        │  + 内置 API 网关 │   │                 │
        └─────────────────┘   └─────────────────┘
```

启动注册任务时，程序会 **re-exec 自身**为一个脱离窗口的 **worker 子进程**（带 `--worker` 参数）。worker 与 UI 完全解耦——即使关闭窗口，注册任务仍在后台继续；`internal/daemon` 通过 PID 文件 + stop 文件管理其生命周期，所以 GUI 和 CLI 可以互相 `Stop` 对方启动的任务。注册成功的账号会通过 `gateway.Sink` 直接写入网关的 SQLite 库，无需手动导入。

---

## 功能总览

桌面控制台左侧导航分为三大区块，共九个工作区：

| 分组 | 工作区 | 说明 |
|------|--------|------|
| **注册** | 总览 | 注册控制台：设定目标数量与并发，启动/停止任务，查看进度与流水线阶段 |
| | 实时日志 | 终端风格日志查看器，可打开日志文件、复制、刷新 |
| | 产物 | 历次运行批次列表，含 CPA/SSO 计数，可在文件管理器中打开 |
| **API 网关** | 网关服务 | 监听配置、状态总览、Token 汇总、可用端点（可复制完整 URL） |
| | 请求日志 | 网关请求实时日志，支持暂停、按状态筛选、关键字搜索、自动滚动 |
| | 账号 | 网关账号池管理：导入 CPA、定时测活、表格/卡片视图、分页、启用/停用、并发上限 |
| | 模型 | 网关当前对外提供的模型列表 |
| | API Keys | 创建、复制、启用、删除网关 API Key |
| **系统** | 设置 | 运行配置：Turnstile、邮箱、代理、CPA 上传等，可打开 `config.env` 编辑 |

窗口为无边框自定义标题栏（可拖拽移动、最小化/最大化/关闭），顶栏在任务运行时显示「任务运行中」状态徽章，底部状态栏展示后台状态、PID、worker 数、数据根目录与版本号。

---

## 系统要求

| 组件 | 版本 | 用途 |
|------|------|------|
| Go | 1.26.5+（以 `go.mod` 为准） | 编译桌面程序与引擎 |
| Node.js | 20+（推荐 22.x） | 构建前端 |
| Microsoft Edge WebView2 Runtime | Windows 10/11 通常已内置 | 渲染桌面 UI |
| Google Chrome / Chromium | 可选，用于原生 Go BrowserPool | Windows 默认 Turnstile 方案 |

> Windows 下 Turnstile 默认走 **原生 Go BrowserPool**，不需要 Python 或 Playwright。

---

## 快速开始（Windows）

### 1. 安装依赖

- **Go**（[下载](https://go.dev/dl/)），并确认加入 `PATH`
- **Node.js**（[下载](https://nodejs.org/)）
- **Google Chrome**（Turnstile 需要，或用 `CHROME_PATH` 指定 Chromium）
- **WebView2 Runtime**（Win10/11 一般已预装）

首次安装 Wails v3 CLI（固定 alpha 版本，与 `go.mod` 一致）：

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
```

### 2. 构建桌面程序

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\build-desktop-windows.ps1
.\bin\grf-desktop.exe
```

构建脚本依次执行：环境检查 → Go 测试 → 生成 Wails 绑定 → 安装并构建 React 前端 → 产出无控制台窗口的 `bin\grf-desktop.exe`。开发时可加 `-SkipTests`、`-SkipInstall` 缩短时间；可用 `-Version` 自定义版本号。

### 3. 首次运行

启动 `grf-desktop.exe` 后：

1. 进入「系统 → 设置」，确认邮箱模式、Turnstile、代理等配置（不熟悉可保持默认）。
2. 回到「注册 → 总览」，填写目标数量与并发线程，点击「开始运行」。
3. 账号注册成功后，在「待人工授权」中点击「开始验证」，并在独立的 xAI 窗口完成登录和设备确认。
4. 授权、探活成功的账号会自动出现在「API 网关 → 账号」中。
5. 在「网关服务」启用网关并保存，即可对外提供 API。

运行数据默认位于 `%USERPROFILE%\.grf`（可用环境变量 `GRF_HOME` 覆盖）。

---

## 使用指南

### 启动注册任务

「总览」页顶部是启动面板：

- **目标数量**：本次要注册的账号数（1–10000）。计数口径是 **CPA 探活成功数**。
- **并发线程**：并行 worker 数（1–8）。
- 点击「开始运行」后，UI 切换为「停止任务」按钮，并显示当前阶段详情。
- 下方为执行流水线，展示六个阶段：网络预热 → Turnstile → 邮箱验证 → 账号注册 → OAuth → CPA 就绪。
- 注册成功的账号会进入「待人工授权」队列。点击「开始验证」后，应用会打开使用原生浏览器指纹的独立 Chrome 窗口；完成 xAI 页面确认后自动继续探活和入库。一次只处理一个授权，其他注册线程可继续工作。
- 人工授权会使用独立的真实 Chrome 窗口，并自动应用注册代理；待授权列表会直接显示注册密码并支持复制。期间需保持桌面应用运行。命令行入口继续使用原有自动 OAuth 流程。

### 实时日志与产物

- 「实时日志」是全高度终端，标题栏显示日志文件路径，可「打开文件」/复制/刷新。
- 「产物」列出最近的运行批次，含 CPA（探活成功）与 SSO 计数，可打开对应目录。

### 账号池管理

「API 网关 → 账号」是功能最丰富的页面：

- **导入 CPA**：从本地 JSON 批量导入账号，返回成功/失败统计。
- **定时测活**：开关 + 间隔（5–1440 分钟）保存后定时检查账号可用性；也可点「立即测活」。
- **表格 / 卡片**两种视图，记忆偏好。
- 每个账号可编辑：启用状态、最大并发上限（1–64），并单条保存或删除。
- 分页支持 12/24/48 每页。

### API Keys

「API 网关 → API Keys」中创建密钥后，**完整密钥只显示一次**（弹窗可复制），列表仅保留前缀。可启用/禁用、再次复制（仅对已保存 secret 的密钥可用）、删除。

---

## 内置 API 网关

GRF 内置一个进程内 HTTP 服务，把账号池封装为 OpenAI / Anthropic 兼容的 API。默认监听 `127.0.0.1:8000`（可在「网关服务 → 监听配置」修改），对所有推理端点强制验证 API Key。

### 可用端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/models` | 模型列表 |
| POST | `/v1/responses` | Responses API |
| POST | `/v1/responses/compact` | Responses（compact） |
| POST | `/v1/chat/completions` | OpenAI Chat 兼容 |
| POST | `/v1/messages` | Anthropic Messages 兼容 |

在「网关服务」页每个端点旁有复制按钮，可一键复制完整地址，例如：

```text
http://127.0.0.1:8000/v1/models
```

### 鉴权

- 密钥格式：`grf_...`
- 请求头：`Authorization: Bearer grf_...`（或 `X-API-Key`，仅 `/v1/messages`）
- 凭据存储：AES-256-GCM（SQLite 落盘）

### Token 与请求监控

- 「网关服务」顶部有 **Token 汇总**（总/输入/输出/请求数），清空请求日志时重置。
- 「请求日志」实时展示每次请求的时间、方法、状态码、延迟、Token、请求内容，支持暂停、按状态（全部/成功/错误）筛选、关键字搜索、自动滚动。

### 调用示例

```bash
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Authorization: Bearer grf_xxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-4.5",
    "messages": [{"role":"user","content":"你好"}]
  }'
```

---

## 前置依赖

注册引擎依赖外部服务来通过 Cloudflare 防护与 Turnstile，这些是 **必需的**（否则注册基本会失败），但与本仓库解耦。

### 清障栈（强烈推荐）

`clearance/` 目录提供一份 Docker Compose，包含 WARP + Privoxy + FlareSolverr，全部监听本机回环：

| 端口 | 服务 |
|------|------|
| `127.0.0.1:40000` | WARP SOCKS5 |
| `127.0.0.1:40080` | Privoxy HTTP（注册 / 浏览器代理） |
| `127.0.0.1:8191` | FlareSolverr |

```powershell
Set-Location clearance
docker compose up -d
docker compose ps
```

> Docker Desktop 需使用 Linux containers + WSL2 backend。`warp-proxy` 容器需要 `NET_ADMIN` 能力；若该容器不可用，可改用外部 HTTP 代理，在设置里填 `REGISTER_PROXY`、`HTTP_PROXY`、`HTTPS_PROXY`。

### Turnstile

- **Windows**：默认 `browser`，自动映射到原生 **Go BrowserPool**，无需 Python；每个 worker 维持一个 Chromium 进程，按需新建隔离 context。可选显式设置 `TURNSTILE_PROVIDER=go-browser`。
- **macOS / Linux**：默认 `browser` 走 Python + Playwright + CloakBrowser；可在设置中改为 `go-browser` 进行 A/B 对比。
- 也可外接 YesCaptcha 形 farm（`TURNSTILE_PROVIDER=lite`，仓库不内置镜像）。

### 邮箱

支持三种模式（在「设置 → 邮箱服务」选择）：

- `tempmail`（默认）：公共 tempmail.lol + mail.tm fallback，无需 token。
- `testmail`：testmail.app，需 API key + namespace。
- `custom`：自建域名邮箱，配合 Cloudflare Email Routing catch-all（参考 `cloudflare/email-worker.js`）。

---

## macOS / Linux 构建

桌面端通过 Wails v3 的 Taskfile 体系跨平台构建（无 PowerShell 脚本）。需先安装 `wails3` CLI，并在仓库根目录执行：

```bash
cd desktop

# 构建（按宿主 OS 自动分发到对应平台 Taskfile）
wails3 task build

# 打包为安装器（macOS app / NSIS / AppImage 等）
wails3 task package

# 以开发模式运行
wails3 task dev

# 以服务端模式运行（无 GUI，仅 HTTP）
wails3 task run:server
```

平台相关 Taskfile 位于 `desktop/build/{windows,darwin,linux}/Taskfile.yml`，可通过 `wails3 build GOOS=linux` 等方式交叉编译。

---

## 开发模式

前端位于 `desktop/frontend`，基于 React 18 + Vite 5，无路由、无 UI 框架、无状态库，全部手写 CSS 与 React hooks。

```bash
cd desktop/frontend
npm install
npm run dev     # 仅前端，热更新（浏览器里可静态预览，无后端时返回 demo 数据）
```

完整的前后端联调热更新用 Wails：

```bash
cd desktop
wails3 task dev          # 默认 dev 端口 9245
```

> 前端通过 `@wailsio/runtime` 的 `Call.ByName` 调用 Go 端 `desktop/app.App` 的绑定方法；在浏览器中（无 `_wails.environment`）每个调用会返回 demo 数据，便于纯前端预览。

---

## 目录结构

```text
GRF/
├── desktop/                      # 桌面端（Wails v3）
│   ├── main.go                   # 入口：worker 分发 + Wails 窗口
│   ├── app/                      # 唯一 Wails 服务 *App，所有前端绑定方法
│   ├── build/                    # Wails 配置与各平台 Taskfile / 打包元数据
│   └── frontend/                 # React + Vite 前端
│       ├── src/
│       │   ├── components/       # 9 个页面 + 布局组件
│       │   ├── lib/native.js     # 后端绑定封装（含 demo 回退）
│       │   ├── styles/app.css    # 全局样式与设计 token
│       │   └── assets/           # logo (svg/png/ico)
│       └── bindings/             # wails3 生成的 TS 绑定
├── cmd/grok/                     # 命令行入口
├── internal/                     # 共享业务包
│   ├── gateway/                  # 内置 API 网关（manager/http/store/selector/health...）
│   ├── pipeline/                 # 注册流水线 S/P/C + OAuth + CPA
│   ├── turnstile/                # Playwright bridge + chromedp + lite
│   ├── clearance/                # FlareSolverr 预热
│   ├── daemon/                   # worker re-exec + PID/stop 文件管理
│   ├── runner/                   # worker 执行体（CLI 与桌面共享）
│   ├── config/                   # 配置加载与 example.env
│   └── ...
├── clearance/                    # 清障栈 docker compose
├── cloudflare/email-worker.js    # 自建邮箱参考
├── scripts/                      # 构建脚本 (build-desktop-windows.ps1 等)
└── go.mod                        # 模块 github.com/grok-free-register/grok-reg（Go 1.26.5）
```

---

## 配置参考

配置保存在 `%USERPROFILE%\.grf\config.env`（可经「设置」页或直接编辑；也可用 `GRF_HOME` 改数据根目录）。主要字段：

### 注册引擎

| 字段 | 默认 | 说明 |
|------|------|------|
| `EMAIL_MODE` | `tempmail` | 邮箱模式：tempmail / testmail / custom |
| `TURNSTILE_PROVIDER` | `browser` | browser / go-browser / playwright / lite / chromedp |
| `REGISTER_PROXY` | `http://127.0.0.1:40080` | 注册与浏览器出口代理 |
| `CLEARANCE_ENABLED` | `1` | 启用 Clearance 预热 |
| `FLARESOLVERR_URL` | `http://127.0.0.1:8191` | FlareSolverr 地址 |

### API 网关

| 字段 | 默认 | 说明 |
|------|------|------|
| `API_ENABLED` | `0` | 是否启用网关 |
| `API_LISTEN_HOST` | `127.0.0.1` | 监听地址 |
| `API_LISTEN_PORT` | `8000` | 监听端口 |
| `API_STREAM_DEFAULT` | `0` | 客户端未传 stream 时是否默认流式 |
| `API_ACCOUNT_HEALTH_ENABLED` | `0` | 定时测活开关 |
| `API_ACCOUNT_HEALTH_INTERVAL_MINUTES` | `30` | 测活间隔 |

### CPA 上传（可选）

成功注册后可自动上传 CPA JSON 到 Management API（`CPA_UPLOAD_ENABLED=1` + `CPA_MANAGEMENT_KEY`）；失败不影响账号计为成功。完整模板见 `internal/config/example.env`。

---

## 命令行版

同一套引擎也提供命令行可执行文件 `grf`，适合无图形界面或脚本化场景：

```bash
grf start -t 10 --thread 3   # 注册 10 个，3 线程
grf status                    # 查看状态
grf logs -f                   # 跟踪日志
grf stop                      # 停止
grf upload                    # 手动上传 CPA JSON
```

构建：

```powershell
.\scripts\build-windows.ps1    # 产出 bin\grf.exe
```

两个版本共享同一份 `config.env`、日志与产物目录，可互换启停彼此的 worker。

---

## 常见问题

**`grf-desktop.exe` 启动后白屏 / 闪退？**
确认已安装 WebView2 Runtime（Win10/11 通常预装）；开发模式用 `wails3 task dev` 查看控制台报错。

**注册卡在 Turnstile / `iframes=0`？**
Windows 下确认 Chrome 已安装或设置 `CHROME_PATH`；确认清障栈已 `docker compose up -d` 且 `REGISTER_PROXY` 可用。

**网关调用 401 / 未授权？**
在「API Keys」创建密钥，调用时带 `Authorization: Bearer grf_...`；确认对应账号在「账号」页已启用且测活通过。

**关闭窗口后任务会停吗？**
不会。注册任务是独立的 worker 子进程，关闭窗口后继续运行；重新打开窗口或用 `grf status` 可查看进度，`grf stop` 可停止。

**只想手动导入 CPA？**
看「账号 → 导入 CPA」，或命令行 `grf upload`。

---

## License

本项目使用 [Apache License 2.0](LICENSE) 授权。第三方组件保留其各自的授权与归属声明，详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

---

## 友链

- [LinuxDo · Charles0509](https://linux.do/u/charles0509)
