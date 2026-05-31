# mattermost-plugin-openusage

Mattermost server-side plugin that exposes [OpenUsage](https://github.com/robinebers/openusage) AI-subscription usage data as private bot cards.

The plugin follows the `mattermost-plugin-codexbar` shape, but reads OpenUsage's
**local HTTP API** directly instead of driving a CLI over rexec:

- Mattermost creates a dedicated `openusage` bot.
- `/openusage` is registered as a plugin slash command.
- The plugin calls OpenUsage's read-only HTTP API (`GET /v1/usage`,
  `GET /v1/usage/:providerId`).
- OpenUsage on the host Mac remains the source of truth; the plugin only renders
  curated, human-readable attachments.
- The bot responds only inside its own direct-message channel.

## Reaching OpenUsage (netbird)

OpenUsage runs on the operator Mac (`macmini.mouriya.lan` on the netbird mesh)
and binds **`127.0.0.1:6736`**; that bind address is not configurable
(`src-tauri/src/local_http_api/server.rs`). The plugin reads its target from the
`OPENUSAGE_BASE_URL` environment variable on the Mattermost server process:

- **Same host as OpenUsage** — leave it unset; it defaults to
  `http://127.0.0.1:6736` (used by `OPENUSAGE_LIVE=1` tests on the Mac).
- **The vctcn Mattermost deployment** — Mattermost runs on VM 180 and reaches the
  Mac over netbird. Because OpenUsage binds loopback only, the Mac must expose
  port 6736 on its netbird interface with a small forwarder LaunchAgent
  (analogous to how `com.mouriya.rexecd.codexbar` already exposes `:50052`).
  `OPENUSAGE_BASE_URL` is then `http://macmini.mouriya.lan:6736` and is injected
  via the Mattermost compose env in `pve-vctcn/apps/vctcn-app1`, alongside
  `CODEXBAR_REXECD_ADDR` — not via the System Console.

  Example forwarder on the Mac:

  ```bash
  socat TCP-LISTEN:6736,bind=macmini-netbird-ip,fork,reuseaddr TCP:127.0.0.1:6736
  ```

## Commands

Run these in the OpenUsage bot DM:

| Command | Effect |
|---|---|
| `/openusage` or `/openusage all` | One card per enabled provider (`GET /v1/usage`). |
| `/openusage <provider>` | One card for a single provider id, e.g. `claude`, `codex` (`GET /v1/usage/:providerId`). |
| `/openusage help` | Show the command surface. |

Provider ids are validated against `^[a-z0-9][a-z0-9-]*$` before they reach the
API path.

## Card contents

Each card renders one OpenUsage provider snapshot:

- **Title** — provider display name plus plan (e.g. `Claude — Max 20x`).
- **Fields** — one per metric line:
  - `progress` → `42% used · resets 2026-05-31 06:50 UTC`, `$12.34 of $100`, or
    `1000 / 1000 credits` depending on the line's `format.kind`
    (`percent` / `dollars` / `count`).
  - `text` / `badge` → the value/text, with any subtitle underneath.
- **Color** — green / amber / red from the worst progress usage in the card
  (≥70% amber, ≥90% red).
- **Footer** — `OpenUsage · <providerId> · fetched <time>`.

## Quick start

```bash
go test ./server/...
COPYFILE_DISABLE=1 make dist
```

Runtime check against a running OpenUsage instance:

```bash
OPENUSAGE_LIVE=1 go test ./server/ -run TestLiveOpenUsage -v
# or, through the netbird forwarder from another mesh host:
OPENUSAGE_LIVE=1 OPENUSAGE_BASE_URL=http://macmini.mouriya.lan:6736 \
  go test ./server/ -run TestLiveOpenUsage -v
```

The bundle is written to `dist/openusage-<plugin.json version>.tar.gz`.

`COPYFILE_DISABLE=1` is required on macOS so the plugin tarball does not contain
`._*` AppleDouble files that break `mmctl --local plugin add`.

## Deploy

The vctcn Mattermost fleet does not install plugins by hand. Delivery mirrors
`mattermost-plugin-codexbar` and is driven entirely by `pve-vctcn`
(`apps/vctcn-app1`):

1. Tag a release here (`vX.Y.Z` matching `plugin.json`). The `release` workflow
   builds the bundle and publishes a GitHub Release with the
   `openusage-X.Y.Z.tar.gz` asset and its `.sha256` sidecar.
2. In `pve-vctcn/apps/vctcn-app1`, bump `mattermost_plugin_openusage_version` to
   the new tag and `tofu apply`. The IaC `null_resource` downloads the release,
   verifies the sha256, `docker cp`s it into the Mattermost container, and runs
   `mmctl --local plugin add --force` + `enable`. It is version-pinned,
   idempotent, and self-heals on every apply (a wiped plugin volume reinstalls).
3. `OPENUSAGE_BASE_URL=http://macmini.mouriya.lan:6736` is set in the Mattermost
   compose env in the same workspace (see "Reaching OpenUsage" above).

No admin personal-access-token and no `make deploy`/`pluginctl` are used in this
path. `make deploy` / `build/pluginctl` remain only as a local break-glass for a
throwaway Mattermost where you hold `MM_ADMIN_TOKEN` directly.
