# GRF Embedded Build API Design

## Status

Accepted on 2026-07-23.

## Goal

Embed the Grok Build portion of `grok2api` into GRF so the desktop application can manage registered OAuth accounts and expose authenticated OpenAI/Anthropic-compatible APIs without launching a separate service.

## Scope

The first release includes:

- Grok Build OAuth accounts produced by the existing GRF pipeline.
- Encrypted local credential storage in SQLite.
- Account availability, quota metadata, enable/disable, and token refresh state.
- Public model catalog and account-aware model routing.
- API keys with the `grf_` prefix, one-time secret display, hashed storage, disable, and delete operations.
- `GET /v1/models`.
- Responses JSON/SSE, compact, stored response retrieval/deletion, Chat Completions, and Anthropic Messages compatibility endpoints.
- Wails pages for accounts, models, API keys, and gateway settings.
- A manually enabled listener. It defaults to `127.0.0.1:8000` and may be changed in Settings.

The first release excludes Grok Web, Grok Console, image/video generation, Redis, PostgreSQL, multi-instance operation, and the original `grok2api` admin frontend.

## Architecture

```text
GRF registration worker -> gateway account sink -> SQLite (WAL)
                                                 ^
Wails bindings -> gateway manager ---------------+
                    |
                    +-> net/http listener -> API key auth -> Build router
                                                        -> Grok Build upstream
```

Code is organized beneath `internal/gateway` with explicit boundaries for domain types, storage, cryptography, Build upstream access, compatibility transports, and lifecycle management. The existing registration pipeline does not depend on HTTP; it calls a narrow account sink after a CPA document has been written successfully. SQLite WAL mode permits the worker process and desktop process to share the database safely.

The implementation selectively ports behavior and tests from the MIT-licensed `grok2api` project. It does not copy the unused Web/Console providers, admin authentication, distributed runtime, or original frontend. Attribution is retained in `THIRD_PARTY_NOTICES.md`.

## Data Model

- `gateway_accounts`: encrypted access/refresh tokens, stable identity, email, expiry, enabled/auth state, concurrency limit, cooldown, observed model, quota snapshot, timestamps.
- `gateway_api_keys`: name, public prefix, SHA-256 secret hash, enabled state, created/last-used timestamps.
- `gateway_models`: public model ID, upstream model, capability flags, enabled state, last observed timestamp.
- `gateway_responses`: locally owned stored response JSON with expiry.
- `gateway_audits`: request identity, model, account, status, latency, and token counters without secrets or full prompt bodies.

The store creates a random 32-byte key at `%GRF_HOME%/gateway/credential.key`. OAuth credentials use AES-256-GCM with a random nonce. API key verification hashes the presented secret and compares fixed-size values in constant time.

## Lifecycle And Security

The gateway is disabled by default. Enabling it validates the listen address, opens/migrates the database, and binds the socket before reporting success. Disabling it stops accepting requests and uses bounded graceful shutdown. A saved enabled state is restored on the next desktop start.

Every `/v1/*` endpoint requires `Authorization: Bearer grf_...`. Health endpoints may remain unauthenticated and disclose no account data. Non-loopback listeners show a warning in the UI, but authentication cannot be disabled. Request bodies are bounded, upstream and shutdown timeouts are explicit, secrets never enter logs, and upstream errors are mapped to stable OpenAI-style error objects.

## Failure Modes

| Failure | Behavior |
| --- | --- |
| Listen address occupied | Enable fails without changing persisted running state. |
| Database migration fails | Gateway remains stopped and returns the migration error to Wails. |
| No eligible Build account | API returns `503` with `no_available_account`. |
| OAuth token expired | Refresh once; permanent refresh failure marks reauthentication required. |
| Upstream quota/rate limit | Account enters bounded cooldown; another eligible account may be selected before request submission. |
| Client disconnects during SSE | Upstream request is canceled and the account lease is released. |
| Desktop exits | Listener receives graceful shutdown; SQLite closes after in-flight operations finish. |

## Verification

Tests cover migrations, encryption, account upsert, API key lifecycle, selector concurrency, token refresh, JSON/SSE proxying, protocol conversions, model listing, response storage, lifecycle restart, and port release. Final verification runs `go test ./...`, frontend lint/build, Wails bindings/build, and local HTTP smoke tests with valid and invalid API keys.
