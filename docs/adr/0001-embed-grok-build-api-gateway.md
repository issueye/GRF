# ADR-0001: Embed the Grok Build API Gateway in GRF

## Status

Accepted

## Context

GRF registers xAI accounts and produces OAuth credentials, while `grok2api` provides mature account scheduling and OpenAI/Anthropic-compatible APIs. Running two separately configured applications duplicates account import, secrets, lifecycle management, and user interfaces. Copying all of `grok2api` would also bring unrelated Web/Console providers, distributed deployment features, and a second admin frontend.

The desktop gateway must be local-first, disabled by default, usable without Python, and able to share newly generated accounts with a registration worker running in a separate process.

## Decision

Selectively port the Grok Build provider and compatibility behavior into a new `internal/gateway` module in GRF. Run the HTTP listener inside the desktop process, persist gateway state in a dedicated SQLite database under the GRF data root, and let the registration worker upsert accounts through the same WAL-enabled store.

Use the existing GRF React/Wails interface for administration. Do not embed the `grok2api` admin frontend or its administrator JWT system. Authenticate every inference endpoint with locally managed `grf_` API keys.

## Consequences

### Positive

- One application owns registration, account inventory, API lifecycle, and configuration.
- New OAuth credentials become immediately available without a separate import service.
- The first release remains smaller than the complete `grok2api` backend.
- API exposure is opt-in and local-only by default.

### Negative

- Selected upstream behavior must be maintained as `grok2api` evolves.
- SQLite and gateway migrations add persistent state to GRF.
- Protocol compatibility, streaming, scheduling, and refresh logic materially increase test scope.

### Neutral

- The gateway uses its own database and encryption key beneath the existing GRF data root.
- Web, Console, media, and distributed deployment may be added later through the same boundaries.

## Alternatives Considered

**Run `grok2api` as a sidecar process**

Rejected because the user selected a single-process embedded backend and a unified GRF interface.

**Copy the complete `grok2api` backend and frontend**

Rejected for the first phase because most code serves excluded providers, distributed deployments, media, and a separate admin experience.

**Rewrite a minimal transparent proxy without porting compatibility behavior**

Rejected because it would not meet the required Responses, Chat Completions, Anthropic Messages, streaming, account scheduling, and stored-response behavior.

## References

- `docs/plans/2026-07-23-embedded-build-api-design.md`
- `E:/code/issueye/ai_agent/grok2api/LICENSE`
- `E:/code/issueye/ai_agent/grok2api/backend/internal/infra/provider/cli`
- `E:/code/issueye/ai_agent/grok2api/backend/internal/application/gateway`
