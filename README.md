# ComfyNexus

ComfyNexus is an authenticated web gateway for a remote ComfyUI instance that is reachable through SSH. It is designed for the common rental-GPU setup where ComfyUI itself has no built-in public authentication, while SSH key access is available.

ComfyNexus 是一个通过 SSH 连接远端 ComfyUI 的带鉴权 Web 网关，面向“租赁 GPU / ComfyUI 裸露公网 / 只有 SSH key 可控”的场景。

## Features / 功能

- Password + TOTP authentication with HttpOnly sessions
- SSH instance management with encrypted inline keys or mounted key files
- ComfyUI HTTP/WebSocket proxy through SSH port forwarding
- SFTP file manager for `/models`, `/input`, `/output`, `/custom_nodes`, and `/user`
- Chunked browser uploads with resumable job state
- Remote URL downloads on the GPU host via `aria2c`, `curl`, or `wget`
- Model library scan, size aggregation, SHA256 hashing, and Civitai metadata sync
- Image library scan, prompt/workflow extraction from PNG metadata, favorites, and ZIP export
- System panel for `nvidia-smi`, ComfyUI process status, restart command, and log tailing
- Chinese and English UI via i18n
- Single Go binary with embedded Vite/React frontend
- Docker/Zeabur-friendly deployment

- 密码 + TOTP 双因素登录，HttpOnly 会话
- GPU 实例配置，可使用加密保存的 SSH key 或挂载 key 文件
- 通过 SSH 隧道反代 ComfyUI 的 HTTP/WebSocket
- 基于 SFTP 的文件管理：`/models`、`/input`、`/output`、`/custom_nodes`、`/user`
- 浏览器分块上传与任务状态记录
- 在 GPU 机器上远端直拉 URL，适合大模型文件
- 模型库扫描、磁盘占用统计、SHA256、Civitai 元数据同步
- 图片库扫描、PNG 工作流/提示词提取、收藏与批量 ZIP
- 系统面板：`nvidia-smi`、ComfyUI 状态、重启命令、日志 tail
- 中英双语 UI
- Go 单二进制内嵌 Vite/React 前端
- 适配 Docker / Zeabur 部署

## Architecture / 架构

```text
Browser
  │ HTTPS
  ▼
ComfyNexus Gateway (Zeabur / VPS)
  ├─ Authenticated web UI and API
  ├─ Embedded React frontend
  ├─ SQLite metadata under /data
  ├─ SSH connection manager
  │   ├─ Port forwarding to ComfyUI 127.0.0.1:8188
  │   ├─ SFTP file operations
  │   └─ Remote exec for nvidia-smi, logs, download jobs
  ▼
Rental GPU host
  └─ ComfyUI bound to 127.0.0.1, not public Internet
```

Recommended remote ComfyUI startup:

```bash
python main.py --listen 127.0.0.1 --port 8188
```

建议把远端 ComfyUI 绑定到 `127.0.0.1:8188`，不要继续公网暴露。

## Tech stack / 技术栈

- Backend: Go, Chi, `golang.org/x/crypto/ssh`, `github.com/pkg/sftp`, SQLite (`modernc.org/sqlite`)
- Frontend: Vite, React, TypeScript, TanStack-style query patterns, Tailwind CSS, i18next
- Deployment: Docker multi-stage build, distroless runtime, Zeabur-compatible `$PORT`

## Quick start / 本地启动

### 1. Configure environment / 配置环境变量

```bash
cp .env.example .env
```

Set at least:

```bash
COMFYNEXUS_MASTER_KEY=$(openssl rand -base64 48)
COMFYNEXUS_DATA_DIR=./data
COMFYNEXUS_SETUP_TOKEN=$(openssl rand -base64 32)
```

For development only, if you do not have the SSH host fingerprint yet:

```bash
COMFYNEXUS_INSECURE_SKIP_HOST_KEY_CHECK=true
```

Production should use host-key pinning instead of this insecure flag.

生产环境应配置 SSH host fingerprint，不建议开启 `COMFYNEXUS_INSECURE_SKIP_HOST_KEY_CHECK`。

### 2. Build and run / 构建运行

```bash
make build
./dist/comfynexus
```

Or run API/frontend separately during development:

```bash
make dev-api
make dev-web
```

### 3. First setup / 首次初始化

Open the service URL and create the first administrator. If `COMFYNEXUS_SETUP_TOKEN` is configured, the setup request must include it. The frontend setup page can send it as the setup token field; API clients may send it via `X-Setup-Token`.

打开服务地址创建第一个管理员。如果配置了 `COMFYNEXUS_SETUP_TOKEN`，初始化请求必须携带该 token。

### 4. Add a GPU instance / 添加 GPU 实例

In **Instances / 实例管理**, configure:

- SSH host, port, username
- SSH key source:
  - Inline encrypted private key
  - Mounted key file path, preferably under `/secrets/ssh`
- SSH host fingerprint (`SHA256:...`)
- ComfyUI host/port, usually `127.0.0.1:8188`
- ComfyUI root path and optional restart command

Then test and activate the instance.

## Zeabur deployment / Zeabur 部署

1. Import this GitHub repository into Zeabur.
2. Use Dockerfile deployment. `zbpack.json` keeps Dockerfile mode enabled.
3. Attach a persistent volume to `/data`.
4. Configure variables:

```env
COMFYNEXUS_MASTER_KEY=<openssl rand -base64 48>
COMFYNEXUS_DATA_DIR=/data
COMFYNEXUS_SETUP_TOKEN=<openssl rand -base64 32>
COMFYNEXUS_TRUST_PROXY=true
COMFYNEXUS_INSECURE_SKIP_HOST_KEY_CHECK=false
CIVITAI_API_KEY=<optional>
HF_TOKEN=<optional>
```

5. If using mounted SSH keys, mount them under a secrets directory such as `/secrets/ssh/id_ed25519` and reference that path from the instance form.

Zeabur 会自动提供 HTTPS 与公网子域名。大模型文件建议使用“远端 URL 直拉”，不要依赖浏览器经由 Zeabur 上传多 GB 文件。

## Security notes / 安全说明

ComfyNexus includes several hardening measures:

- Strict SSH host-key verification by default
- Optional first-run setup token
- Encrypted inline SSH keys at rest
- Hashed session tokens in SQLite
- HttpOnly/Secure/SameSite cookies
- JSON and upload chunk size limits
- File-manager sandbox limited to known ComfyUI directories
- Protection against deleting `/` and protected top-level directories
- Proxy header isolation: ComfyNexus strips `Cookie`, `Authorization`, `X-Requested-With`, setup-token, and internal headers before proxying to ComfyUI, and strips upstream `Set-Cookie`
- Security headers/CSP on the admin UI

安全加固包括：SSH host key 校验、首次初始化 token、SSH key 加密落盘、会话 token 哈希、Cookie 安全属性、请求体限制、文件管理目录沙箱、危险删除保护、反代敏感头隔离，以及管理界面安全响应头。

Important: ComfyUI and its custom nodes are still powerful and may execute arbitrary UI code. Only install custom nodes you trust. For the strongest isolation, deploy ComfyUI proxy access on a separate origin/subdomain and keep the admin API cookie scoped away from that origin.

重要：ComfyUI 与 custom nodes 本身仍然很强大，可能包含任意前端代码。只安装可信插件。若需要更强隔离，建议把 ComfyUI 反代访问放到单独子域名，并避免管理 API Cookie 覆盖该域名。

## Main API areas / 主要 API

- `/api/auth/*` — setup, login, logout, current user
- `/api/instances/*` — SSH/GPU instance CRUD and activation
- `/api/files/*` — SFTP file management
- `/api/uploads/*` — chunked browser uploads
- `/api/downloads/*` — remote URL download jobs
- `/api/models/*` — model scan, Civitai sync, disk usage
- `/api/images/*` — output image scan, workflow extraction, ZIP export
- `/api/system/*` — GPU telemetry, ComfyUI status, restart, logs
- `/comfy/*` and `/ws/*` — authenticated ComfyUI proxy routes

## Quality checks / 质量检查

```bash
make quality
go build ./...
```

`make quality` runs race-enabled Go tests and a clean frontend production build.

## Repository layout / 目录结构

```text
cmd/comfynexus/       # entrypoint
internal/auth/        # password, TOTP, sessions
internal/config/      # env/config loading
internal/db/          # SQLite migrations
internal/proxy/       # ComfyUI HTTP/WS proxy
internal/server/      # API handlers
internal/sftpx/       # SFTP helpers and path safety
internal/sshmgr/      # SSH connection manager
internal/tunnel/      # SSH port forwarding
internal/web/         # embedded frontend assets
web/                  # Vite/React frontend source
```

## License / 许可证

MIT
