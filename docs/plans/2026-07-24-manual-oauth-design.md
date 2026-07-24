# Manual OAuth Design

## Goal

Replace the post-registration HTTP approval shortcut with an explicit user-driven
xAI device authorization flow. Registered accounts may continue to queue while
the desktop authorizes one account at a time in a dedicated Chrome window.

## Architecture

The detached registration worker and the Wails process communicate through one
atomic JSON task file per account under `<run>/oauth/`. The worker writes a
queued task after registration and waits for an authorized credential. The
desktop lists those tasks, starts the device flow only after the user clicks
Authorize, opens the returned verification URL in a real Chrome app window, and
polls the token endpoint. A successful token response is written back to the
task file; the worker then resumes onboarding, probing, CPA output, and gateway
import.

Only one desktop authorization is active at a time. Registration workers may
continue until all target seats are either complete or waiting for OAuth. This
avoids expiring device codes while preserving registration concurrency.

## Security And Browser Profile

Task files use owner-only permissions. The local Wails DTO includes the account
password so the operator can view and copy it during manual login, while SSO
cookies and OAuth credentials remain omitted. OAuth Chrome uses its native browser
fingerprint without user-agent or navigator overrides. It runs in app mode with
a dedicated profile under `%GRF_HOME%/oauth-browser`, isolated from the user's
normal Chrome profile. The worker removes the task file after the credential has
been consumed.

The device-flow client and OAuth Chrome both receive the configured registration
proxy. An empty or invalid value falls back to `http://127.0.0.1:40080`, while
localhost, `127.0.0.1`, and `::1` bypass the Chrome proxy.

## Failure Handling

Closing the Chrome window cancels the current poll and returns the task to a
retryable failed state. Expired or denied device codes are also retryable. A
worker stop cancels its queued task and releases the reserved target seat.
Restarting the desktop converts abandoned `authorizing` tasks to `failed`, so a
new device flow can be started without re-registering the account.

## Verification

- Unit-test atomic task creation, public redaction, transitions, and waiting.
- Unit-test the pipeline's manual OAuth handoff with a temporary store.
- Unit-test Chrome launch arguments, proxy fallback, and native user-agent use.
- Generate Wails bindings and build the React frontend.
- Run all Go tests and a desktop development build.
