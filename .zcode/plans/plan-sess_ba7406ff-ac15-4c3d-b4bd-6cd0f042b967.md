## Castle Request Token 实现计划

图片方案核心：注册请求缺 Castle request token（发码缺 field 3、建号 `castleRequestToken` 为空），导致账号信任分低、OAuth 易 `invalid_grant`。本计划为 GRF 增加**真实 Castle token minting** 并正确放入两个注册请求。

### 一、Castle SDK 加载与 minting（浏览器侧）

Castle SDK 必须在真实浏览器里跑（它做指纹采集）。**复用现有 turnstile 浏览器**——它已导航到 `accounts.x.ai/sign-up`、具备反自动化设置，同一会话里顺带 mint Castle，零额外浏览器成本。

**`internal/turnstile/browser.go`**：在 `Browser.Solve` 拿到 turnstile token 后、返回前，新增 `mintCastleToken(tabCtx)`：
- 主动 fetch CDN SDK（不依赖页面是否加载）：JS 里 `fetch('https://js.castle.io/castle.js')` 或 `d2a8a4nxofan8h.cloudfront.net/v2/castle.js`，拿到源码后 `(0, eval)(src)`（避免 `script.textContent` IIFE 不执行）。
- `_castle('setAppId', CASTLE_PK)`，其中 `CASTLE_PK = "pk_p8GGwVD3TmFJZRsX3BQcqAv9aFVispNz"`（定义为包常量）。
- `await _castle('createRequestToken')`，读返回字符串。
- 超时 8s，**失败不阻断 turnstile**（Castle 失败仍返回 turnstile token，castle 置空）——保证降级可用。

**改 `Solve` 返回签名**为 `(turnstile string, castle string, error)`，或更稳妥地**新增结构体**避免大改调用方。权衡后采用：新增 `SolveFull(ctx, siteKey, pageURL) (SolveResult, error)`，`SolveResult{Token, Castle string}`；保留 `Solve` 调 `SolveFull` 仅返回 turnstile（兼容 chromedp/lite 等非浏览器 provider，它们 `SolveFull` 返回空 castle）。

**Provider 接口**（`turnstile.go`）：新增可选接口 `CastleMinter interface { SolveFull(ctx, siteKey, pageURL string) (SolveResult, error) }`。BrowserPool/Browser 实现；chromedp/lite/playwright 不实现（类型断言失败则 castle 为空，pipeline 降级）。

### 二、token 流转（pipeline）

**`internal/pipeline/pipeline.go`**：
- `inventory.Inventory[string, QItem]` 的 T slot 现存 turnstile token（`string`）。改为存 `SolveResult`（`{Token, Castle}`），或更小侵入：**新增并行的 castle inventory**。权衡：改 T 的值类型最干净。但 Inventory 是泛型 `Inventory[T, Q]`，T=string。改 T=`turnstile.SolveResult` 需改 `sWorker`/`cWorker` 的 `.Value` 用法。
  - **最小侵入方案**：在 `Engine` 加 `castleTokens` 简单 LIFO（`sync.Mutex + []string`），`sWorker` mint 后 `pushCastle(castle)`，`cWorker` 取号前 `popCastle()`。问题：turnstile 与 castle 可能来自不同 worker，但二者都是"一次性凭据"，配对无强约束（castle 不绑 turnstile），LIFO 可接受。**但更稳妥是保证同批**。
  - **推荐方案**：改 T slot 值类型。评估后：T 泛型改 `turnstile.SolveResult`，影响面：`inv.PutT(ctx, solveRes, ttl)`、`pair.T.Value.Token/.Castle`。约 3-4 处，清晰且保证配对。采用此方案。

- `cWorker`：取 `pair.T.Value` 得 `{Token, Castle}`；`CreateEmailCode` 传入 castle；`BuildSignupBody` 传入 castle。

### 三、发码请求加 Castle（protobuf field 3）

**`internal/protocol/xai.go` `CreateEmailCode`**：
- 当前 `inner := pbStr(1, email)`。改为：
  ```go
  inner := pbStr(1, email)
  if castleToken != "" {
      inner = append(inner, pbStr(3, castleToken)...)
  }
  ```
- 签名改为 `CreateEmailCode(ctxOrEmail...)` —— 实际是 `CreateEmailCode(email, castle string)`。改唯一调用点 `pipeline.go:566`。

### 四、建号请求加 Castle（JSON castleRequestToken）

**`internal/protocol/xai.go` `BuildSignupBody`**：
- 签名加 `castleToken string` 参数，`"castleRequestToken": ""` → `"castleRequestToken": castleToken`。
- 改唯一调用点 `pipeline.go:623`。

### 五、验证
- `go build ./...`、`go vet ./internal/...`、`go test ./internal/protocol/... ./internal/pipeline/... ./internal/turnstile/...`。
- protobuf field3 编码：用现有 `pbStr(3, castle)`（wire type 2，长度前缀），与 field1 同理，无需新 helper。

### 风险与降级
- Castle CDN 被拦 / mint 超时 → castle 置空，注册仍走（与现状等价），不会比现在更差。
- 改 T 泛型值类型是唯一有回归风险点，靠 build + 既有 turnstile 测试覆盖。
- 不改前端、不加 config 开关（Castle 是注册必需，默认启用；失败自动降级）。

### 不做
- 不引入 Python 路径 mint Castle（Windows 走原生 Go pool，已在 browser.go）。
- 不动 VerifyEmailCode（方案只提发码 field3 + 建号 field11/JSON）。