# slio

A read-only Slack CLI built for AI coding agents.

When you ask an AI agent about a Slack discussion, the usual workflow is to take screenshots of the thread and paste them in. `slio` turns that round-trip into a single command: give the agent a thread URL (or a channel, or a search query) and it gets the content back as AI-readable Markdown.

- **Read-only by design** — no write scopes are ever requested, so unintended posting is structurally impossible.
- **User-token based** — covers public/private channels and DMs alike: whatever you can see in Slack, `slio` can read.

## Install

With [Homebrew](https://brew.sh) on macOS:

```sh
brew install 178inaba/tap/slio
```

Or download the archive for your OS and architecture from the [Releases page](https://github.com/178inaba/slio/releases), unpack it, and put the `slio` binary somewhere on your `PATH`.

Or, with a Go toolchain:

```sh
go install github.com/178inaba/slio@latest
```

Check it with `slio --version`.

### Verify a download

After downloading an archive:

```sh
gh attestation verify <downloaded archive> --repo 178inaba/slio
```

A pass proves the archive was built by this repository's release workflow from the tagged commit, and has not been altered since.

## Setup

`slio` uses a Slack user token (`xoxp-...`). Each user creates their own Slack app in their own workspace from the manifest bundled in this repo, so the app stays "internal" and keeps the higher rate limits internal apps get.

1. Go to [api.slack.com/apps](https://api.slack.com/apps) → **Create New App** → **From a manifest**.
2. Select your workspace, paste the contents of [`slack-app-manifest.yml`](./slack-app-manifest.yml), and create the app.
3. Under **OAuth & Permissions**, click **Install to Workspace** and approve.
4. Copy the **User OAuth Token** (starts with `xoxp-`).
5. Register it with `slio`:

   ```sh
   slio auth login
   ```

   Paste the token when prompted — it stays hidden as you type, so nothing appears on screen. `slio` verifies it and records the workspace as a profile (the first one registered becomes the default).

The manifest is only a scope template: it contains no secrets and no workspace-specific values, so it's safe to share and reuse across workspaces.

## Usage

```sh
# Fetch a full thread from its Slack URL (message permalink or reply permalink)
slio thread https://myworkspace.slack.com/archives/C0123456789/p1234567890123456

# Fetch recent channel history — by URL, ID, or #name
slio history '#general'
slio history C0123456789 --since 24h --limit 100

# Search messages — Slack's search syntax passes through unchanged
slio search 'in:#general from:@alice deploy'

# List channels visible to the token
slio channel list

# Every invocation has a 90s deadline; raise it for a large fetch
slio history '#general' --limit 1000 --timeout 5m

# Manage workspace profiles
slio profile list
slio profile use otherworkspace
```

Add `--format json` to any read command for structured output instead of Markdown. Add `--download` to `slio thread` to save attachments locally and print their paths. `slio thread` also marks the one message the URL points at — a trailing `🎯 _linked message_` on its header line in Markdown, `"linked": true` in JSON — so a reply permalink pins the reply within the thread it belongs to. Run `slio --help` or `slio <command> --help` for the full flag reference.

### Multiple workspaces

Commands that take a URL (`slio thread`, and `slio history` when given a URL) pick the right profile automatically from the URL's host. Commands without one (`slio history` given a channel ID or `#name`, `slio search`, `slio channel list`) use the default profile; pass `--profile <name>` to use another one, or `slio profile use <name>` to change the default.

### Environment variables

- `SLIO_TOKEN` overrides the stored token for a single invocation, bypassing profile resolution.
- `SLIO_PROFILE` selects a profile without passing `--profile` every time.
- `SLACK_TOKEN` is intentionally **not** read — it's a shared namespace other tools populate with bot tokens, which would cause confusing partial failures (e.g. only `search` failing, since `search.messages` requires a user token).

### Output streams

Commands write machine-readable output — Markdown, JSON, and the profile and channel listings — to stdout. Prompts, confirmations, and status messages go to stderr, so `slio ... | jq` and `slio ... > thread.md` stay clean.

`slio auth login` is interactive: it masks the token as you paste it and needs a terminal, so it refuses a piped stdin. Use `SLIO_TOKEN` in non-interactive environments.

### Timeouts and exit codes

`--timeout` sets an overall deadline for the invocation and takes a Go duration (`90s`, `5m`), defaulting to 90 seconds. `--timeout 0` disables the deadline. The deadline covers the requests, not the time you spend at a prompt.

Ctrl-C and `SIGTERM` are not reported as failures: slio prints nothing and terminates by the signal, so a shell reports `130` for Ctrl-C and `143` for `SIGTERM`, and a Ctrl-C inside a loop over slio invocations ends the loop too. At an `slio auth login` prompt the terminal's echo is restored first, and no profile is written or changed.

Failures print once to stderr as `Error: ...`, and the exit code says which kind it was:

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `124` | The `--timeout` deadline expired — re-run with a longer one (the code follows the GNU `timeout` convention) |
| `1` | Everything else |

## Agent Skill

This repo ships an [Agent Skill](./skills/slio/SKILL.md) that tells an AI agent when and how to reach for `slio`. In Claude Code, install it as a plugin:

```sh
claude plugin marketplace add 178inaba/slio
claude plugin install slio@slio
```

For other agents, use a skill installer that consumes GitHub repos directly, e.g. [`npx skills`](https://www.npmjs.com/package/skills):

```sh
# The URL form is required: the 178inaba/slio@stable shorthand resolves the tag
# at install time but does not record it, so `npx skills update` silently falls
# back to the default branch.
npx skills add https://github.com/178inaba/slio/tree/stable
```

## Development

```sh
go build ./...
go test -race ./...

# Lint runs in Docker so the version matches CI — see compose.yaml
docker compose run --rm lint

# Let golangci-lint apply the fixes it can make itself
docker compose run --rm lint --fix
```

## Out of scope

`slio` never posts, reacts, or uploads — replies are drafted by the AI and posted by the human. Canvases and other non-message content, browser-session tokens (`xoxc`/`xoxd`), and OS keychain storage are also out of scope. See [the tracking issue](https://github.com/178inaba/slio/issues/1) for the full list.
