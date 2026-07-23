# Embedded Grok Build API Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Embed an opt-in Grok Build API gateway in GRF with encrypted account storage, `grf_` API keys, compatible inference endpoints, and Wails management pages.

**Architecture:** Add a self-contained `internal/gateway` module backed by SQLite WAL. The desktop owns the HTTP lifecycle and Wails management bindings, while the registration worker writes successful OAuth credentials through a narrow store API. Protocol code selectively ports Build behavior from the MIT-licensed `grok2api` source.

**Tech Stack:** Go 1.26, `net/http`, `database/sql`, modernc SQLite, AES-GCM, Wails v3, React 18, Vite.

---

### Task 1: Storage and encryption foundation

**Files:**
- Create: `internal/gateway/domain.go`
- Create: `internal/gateway/crypto.go`
- Create: `internal/gateway/store.go`
- Create: `internal/gateway/migrations.go`
- Test: `internal/gateway/crypto_test.go`
- Test: `internal/gateway/store_test.go`
- Modify: `go.mod`

**Steps:**

1. Write failing tests for key creation permissions, AES-GCM round trips, malformed ciphertext, schema migration, account upsert identity, and concurrent readers/writers.
2. Run `go test ./internal/gateway -run 'Test(Cipher|Store)'` and verify the package is missing or tests fail.
3. Implement the minimum domain, cipher, migration, and SQLite store code with WAL and busy timeout.
4. Run the focused tests and verify they pass.
5. Run `go test ./...` to detect integration regressions.

### Task 2: API key lifecycle

**Files:**
- Create: `internal/gateway/keys.go`
- Test: `internal/gateway/keys_test.go`

**Steps:**

1. Write failing tests for `grf_` generation, one-time secret return, hashed persistence, constant-time verification, disable, delete, and last-used updates.
2. Run the focused test and verify failure.
3. Implement the API key service on the store transaction boundary.
4. Run the focused test and full gateway package tests.

### Task 3: Build account import and selection

**Files:**
- Create: `internal/gateway/build/import.go`
- Create: `internal/gateway/selector.go`
- Test: `internal/gateway/build/import_test.go`
- Test: `internal/gateway/selector_test.go`
- Reference: `E:/code/issueye/ai_agent/grok2api/backend/internal/infra/provider/cli/import.go`

**Steps:**

1. Write failing compatibility tests using current `cpa.Document` JSON and `grok2api` Build import fixtures.
2. Implement normalization for `expired` and `expires_at`, stable identity, encrypted upsert, enabled/auth state, leases, cooldown, and least-recently-used selection.
3. Verify selection never exceeds per-account concurrency and always releases leases after cancellation.
4. Run focused and full Go tests.

### Task 4: Gateway lifecycle and authenticated model API

**Files:**
- Create: `internal/gateway/manager.go`
- Create: `internal/gateway/http.go`
- Create: `internal/gateway/models.go`
- Test: `internal/gateway/manager_test.go`
- Test: `internal/gateway/http_test.go`

**Steps:**

1. Write failing tests for default-disabled status, listen validation, occupied port, graceful shutdown, port release, missing/invalid/disabled key responses, and `GET /v1/models`.
2. Implement `Manager.Start`, `Manager.Stop`, status snapshots, bounded request bodies, JSON errors, auth middleware, and model catalog.
3. Run focused tests and `go test ./...`.

### Task 5: Grok Build OAuth refresh and Responses proxy

**Files:**
- Create: `internal/gateway/build/client.go`
- Create: `internal/gateway/build/oauth.go`
- Create: `internal/gateway/responses.go`
- Test: `internal/gateway/build/client_test.go`
- Test: `internal/gateway/responses_test.go`
- Reference: `E:/code/issueye/ai_agent/grok2api/backend/internal/infra/provider/cli/adapter.go`
- Reference: `E:/code/issueye/ai_agent/grok2api/backend/internal/infra/provider/cli/oauth.go`

**Steps:**

1. Write failing tests with an upstream `httptest.Server` for header construction, JSON forwarding, SSE flushing, cancellation, token refresh, permanent refresh failure, and pre-submission failover.
2. Implement the Build client and `/v1/responses` handler without logging tokens or bodies.
3. Map upstream failures to stable OpenAI error objects and update account cooldown/auth state.
4. Run focused tests, race tests for the gateway package, and full Go tests.

### Task 6: Compatibility protocols and stored responses

**Files:**
- Create: `internal/gateway/compat/chat.go`
- Create: `internal/gateway/compat/messages.go`
- Create: `internal/gateway/compat/stream.go`
- Create: `internal/gateway/response_store.go`
- Test: `internal/gateway/compat/chat_test.go`
- Test: `internal/gateway/compat/messages_test.go`
- Test: `internal/gateway/response_store_test.go`
- Reference: `E:/code/issueye/ai_agent/grok2api/backend/internal/transport/http/inference`

**Steps:**

1. Port representative `grok2api` fixtures for text, reasoning, tool calls, usage, JSON/SSE, compact, retrieval, and deletion.
2. Implement Chat Completions and Anthropic Messages request/response conversion around the common Responses execution path.
3. Implement bounded stored responses with ownership checks and expiry cleanup.
4. Run compatibility tests and full Go tests.

### Task 7: Registration account sink

**Files:**
- Modify: `internal/pipeline/pipeline.go`
- Modify: `internal/runner/worker.go`
- Create: `internal/gateway/sink.go`
- Test: `internal/pipeline/pipeline_test.go`
- Test: `internal/gateway/sink_test.go`

**Steps:**

1. Write a failing test proving a successful CPA document is offered to an optional account sink exactly once and sink failure does not discard the CPA output.
2. Add the narrow sink interface and open the shared gateway store from the worker process.
3. Log import failures without credentials and preserve registration success semantics.
4. Run pipeline, gateway, and full Go tests.

### Task 8: Wails lifecycle and management bindings

**Files:**
- Create: `desktop/app/gateway.go`
- Modify: `desktop/app/app.go`
- Modify: `desktop/main.go`
- Modify: `internal/config/config.go`
- Modify: `config.env.example`
- Modify: `internal/config/example.env`
- Test: `desktop/app/gateway_test.go`
- Test: `internal/config/config_test.go`

**Steps:**

1. Write failing tests for default-disabled configuration, address/port persistence, status, account/model/key DTOs, and app shutdown.
2. Add Wails methods for gateway settings, enable/disable, account list/update, model list, and API key CRUD.
3. Restore the saved enabled state during desktop startup and stop gracefully on application shutdown.
4. Regenerate Wails bindings and run full Go tests.

### Task 9: GRF management pages

**Files:**
- Create: `desktop/frontend/src/components/AccountsPage.jsx`
- Create: `desktop/frontend/src/components/ModelsPage.jsx`
- Create: `desktop/frontend/src/components/APIKeysPage.jsx`
- Modify: `desktop/frontend/src/components/SettingsPage.jsx`
- Modify: `desktop/frontend/src/components/Sidebar.jsx`
- Modify: `desktop/frontend/src/App.jsx`
- Modify: `desktop/frontend/src/lib/native.js`
- Modify: `desktop/frontend/src/styles/app.css`

**Steps:**

1. Add browser-preview fixtures for gateway settings, accounts, models, and keys.
2. Implement dense operational tables, empty/error/loading states, enable confirmation, one-time key secret dialog, copy action, disable, and delete flows.
3. Run `npm run build` from `desktop/frontend`.
4. Start `wails3 dev`, inspect desktop and narrow viewport screenshots, and fix overflow or state issues.

### Task 10: End-to-end verification and attribution

**Files:**
- Create: `THIRD_PARTY_NOTICES.md`
- Modify: `README.md`
- Test: `internal/gateway/e2e_test.go`

**Steps:**

1. Add the `grok2api` MIT notice and document gateway configuration and curl examples.
2. Start the gateway on a temporary loopback port, create a key, import a fixture Build account, and verify unauthorized, models, Responses JSON, and Responses SSE behavior against a fake upstream.
3. Run `go test ./...`, `go test -race ./internal/gateway/...`, `go vet ./...`, frontend build, and `wails3 build DEV=true`.
4. Start `wails3 dev`, verify the gateway is initially disabled, enable it, call `/v1/models`, disable it, and verify the port is released.
