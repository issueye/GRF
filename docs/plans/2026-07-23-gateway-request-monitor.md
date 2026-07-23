# Gateway Request Monitor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an in-memory, console-style monitor for live gateway requests without persisting sensitive request data.

**Architecture:** Wrap the gateway router with an observation middleware that appends sanitized request metadata to a mutex-protected 500-entry ring buffer. Expose snapshot and clear operations through the desktop Wails service, then poll snapshots from a dedicated React page while it is visible.

**Tech Stack:** Go `net/http`, Wails v3 bindings, React 18, Lucide icons, existing GRF CSS.

---

### Task 1: In-memory request log

**Files:**
- Create: `internal/gateway/request_log.go`
- Create: `internal/gateway/request_log_test.go`
- Modify: `internal/gateway/manager.go`
- Modify: `internal/gateway/http.go`

**Steps:**
1. Write tests for the 500-entry bound, newest-first snapshots, clear, status capture, and streaming `http.Flusher` support.
2. Run `go test ./internal/gateway -run RequestLog -count=1` and verify the tests fail.
3. Implement the synchronized buffer and response-writer middleware.
4. Attach the middleware around the gateway router and annotate model/account metadata inside inference handling.
5. Run `go test ./internal/gateway -count=1` and verify it passes.

### Task 2: Desktop service operations

**Files:**
- Modify: `desktop/app/app.go`
- Regenerate: `desktop/frontend/bindings/`

**Steps:**
1. Add `ListGatewayRequestLogs(limit int)` and `ClearGatewayRequestLogs()` methods.
2. Return an empty list when the gateway store is unavailable only if no manager exists; otherwise surface initialization errors.
3. Regenerate Wails bindings and verify both methods are present.

### Task 3: Console monitor page

**Files:**
- Create: `desktop/frontend/src/components/GatewayLogsPage.jsx`
- Modify: `desktop/frontend/src/components/Sidebar.jsx`
- Modify: `desktop/frontend/src/lib/native.js`
- Modify: `desktop/frontend/src/App.jsx`
- Modify: `desktop/frontend/src/styles/app.css`

**Steps:**
1. Add a dedicated `网关日志` navigation item and native preview functions.
2. Poll once per second only while the page is active.
3. Add pause/resume, clear, status filter, keyword search, and auto-scroll controls.
4. Render a fixed-height terminal with timestamp, method, path, status, latency, model, account ID, and a sanitized User-Agent.
5. Run `npm run build` and fix all compile/layout errors.

### Task 4: Final verification

**Steps:**
1. Run `go test ./...`.
2. Run `go test -race ./internal/gateway`.
3. Run `npm run build` in `desktop/frontend`.
4. Run `wails3 generate bindings` and `wails3 build DEV=true` in `desktop`.
5. Confirm the updated executable exists at `desktop/bin/GRF.exe`.
