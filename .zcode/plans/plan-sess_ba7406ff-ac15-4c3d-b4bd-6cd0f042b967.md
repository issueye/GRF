## 账号 Onboarding（TOS / 生日 / NSFW）实现计划

移植 grok-register 的账号激活 3 步调用到 GRF：注册拿到 SSO 后，补做「接受 TOS → 设置生日 → 开启 NSFW」，让新号完整可用。**失败不阻断注册成功**（与探活同语义），仅记日志。

### 调用细节（逐字节对齐参考实现 `registration_browser.py:68-188`）

| 步骤 | URL | 方法/体 | 关键头 |
|------|-----|---------|--------|
| TOS | `https://accounts.x.ai/auth_mgmt.AuthManagement/SetTosAcceptedVersion` | grpc-web，inner = `field2(bool=true)` = `0x10 0x01`（标签 0x10=field2 wire0，值 0x01）→ 外包 `grpcWebFrame` | `content-type: application/grpc-web+proto`, `x-grpc-web: 1`, `x-user-agent: connect-es/2.1.1`, `origin: https://accounts.x.ai`, `referer: https://accounts.x.ai/accept-tos` |
| 生日 | `https://grok.com/rest/auth/set-birth-date` | JSON `{"birthDate":"<随机 20-40 岁 ISO>"}` | `content-type: application/json`, `origin/referer: https://grok.com` |
| NSFW | `https://grok.com/auth_mgmt.AuthManagement/UpdateUserFeatureControls` | grpc-web，inner = `field1{0x10,0x01} + field2{"always_show_nsfw_content"}`（嵌套长度前缀）→ 外包 `grpcWebFrame` | `content-type: application/grpc-web+proto`, `x-grpc-web: 1`, `origin/referer: https://grok.com` |

所有请求带 cookie `sso=<token>; sso-rw=<token>`（+可选 `cf_clearance`）、`user-agent`（来自 clearance）、走 `REGISTER_PROXY`。成功 = HTTP 2xx；403/429/503+cloudflare 视为 CF 拦截（复用 GRF 既有 `isCloudflare` 判定思路）。

**关键：TOS 的 grpc 字段是 `field2` 不是 `field1`**（参考 `struct.pack("B",(2<<3)|0)+struct.pack("B",1)` = `0x10 0x01`）。NSFW 是两层嵌套 message。GRF 现有 `pbStr`/`pbVarint`/`grpcWebFrame` 复用，但 NSFW 需要新的 `pbBytes(field, nested)` 辅助（长度前缀的嵌套 message）。

### 一、新建包 `internal/onboarding/`（自包含、可测）

**`onboarding.go`**：
- `type Options struct { Proxy, UserAgent string }`
- `type Result struct { TOS, BirthDate, NSFW bool }`
- `func Run(ctx, sso, cfClearance string, opt Options, log *logx.Logger) (Result, error)`：内部建 `*http.Client`（proxy+InsecureSkipVerify，复用 protocol 的 transport 构造模式），依次执行 3 步，**逐步容错**（TOS 失败不跳过后续——参考实现是逐步短路返回，但 GRF 语义改为"尽力而为"：每步独立记录，不中断），返回每步成败 + 首个错误。
- 三步函数 `setTOS` / `setBirthDate` / `updateNSFW`，各自构造请求、发 POST、判 2xx/CF。
- `encodeNSFW()` 产出 grpc 字节（带 `pbBytes` 嵌套辅助）。

设计取舍：**独立 `http.Client`**，不挂在 protocol.Client 上——因为 onboarding 走 `grok.com` 域（protocol 走 `accounts.x.ai`），cookie/header 语义不同（手动 `Cookie` 头而非 jar），且要在 OAuth 成功后单独触发。复用 `logx.Logger` 记录每步。

### 二、集成点 `internal/pipeline/pipeline.go`（oauthWorker）

在 `oauthWorker` 里 **OAuth `Exchange` 成功之后、`tryComplete` 之前**插入 onboarding（line 675 `e.oaN.Add(1)` 之后）：
```go
if e.opt.Cfg.OnboardingEnabled {
    onbRes, onbErr := onboarding.Run(ctx, job.SSO, e.cfClearance(), onboarding.Options{
        Proxy: e.opt.Cfg.RegisterProxy, UserAgent: e.userAgent(),
    }, log)
    if onbErr != nil {
        log.Warnf("onboarding 部分失败 %s: %v (TOS:%v 生日:%v NSFW:%v)", ...)
        // 不 releaseReserve，不 fail++ —— 账号仍算成功
    } else {
        log.OKf("onboarding 完成 %s", job.Email)
    }
}
```
- `e.cfClearance()`：新增小方法，从 `e.cm` 的 `Get().Cookies` 里找 `cf_clearance`（与 `clearCookieHeader` 同源）。无 clearance 时返回空串。
- `e.userAgent()`：返回 `e.cm.UserAgent()` 或 `protocol.DefaultUserAgent`。
- **放置时机选择**：放在 OAuth 之后而非参考的"注册后立即"——理由：①此时 SSO 已被 OAuth 验证有效；②GRF 的 C 阶段（注册）与 OAuth 是分离 worker，把激活放在 oauthWorker 内聚更清晰；③失败不阻断，时机不影响主流程。记录中会注明与参考实现的时机差异。

### 三、配置开关 `internal/config/config.go` + `example.env`

- Config struct 加 `OnboardingEnabled bool`（紧挨 `ProbeEnabled bool`，line 56）。
- `Defaults()` 加 `OnboardingEnabled: true`（line 101 后）——默认开启，因为这是新号必需的激活。
- `writeEnv`：加 `ONBOARDING_ENABLED=%s`（line 166 后）。
- `applyMap`：加 `ONBOARDING_ENABLED` → `truthy`（line 368 后）。
- `example.env`：加 `ONBOARDING_ENABLED=1` 及注释（line 127 PROBE 附近）。

### 四、测试

- `internal/onboarding/onboarding_test.go`：用 `httptest.Server` 验证 ① 三个端点被按序调用 ② TOS/NSFW 的 grpc 字节精确等于参考值（golden bytes：TOS=`00 00 00 00 02 10 01`，NSFW=`00 00 00 0F 0A 02 10 01 12 19 0A 17 ...always_show_nsfw_content`）③ 生日 JSON 含 `birthDate` 且年龄在 20-40 ④ 失败返回对应 result 位。
- 不写 pipeline 集成测试（涉及 mock 太多），靠 onboarding 单测 + go build/vet 保证接线正确。

### 五、验证
- `go build ./...`、`go vet ./...`、`go test ./internal/onboarding/... ./internal/config/... ./internal/pipeline/...`。

### 不做的事（保持聚焦）
- 不改前端（开关在 config.env，默认开启，无需 UI）。
- 不引入 curl_cffi/uTLS 指纹伪装（独立的后续项）。
- 不改注册/OAuth 主流程，onboarding 纯增量、失败不阻断。