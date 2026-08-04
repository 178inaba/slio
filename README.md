# slio

A read-only Slack CLI built for AI coding agents.

When you ask an AI agent about a Slack discussion, the usual workflow is to take screenshots of the thread and paste them in. `slio` turns that round-trip into a single command: give the agent a thread URL (or a channel, or a search query) and it gets the content back as AI-readable Markdown.

- **Read-only by design** — no write scopes are ever requested, so unintended posting is structurally impossible.
- **User-token based** — covers public/private channels and DMs alike: whatever you can see in Slack, `slio` can read.

## Install

```sh
go install github.com/178inaba/slio@latest
```

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

   Paste the token when prompted. `slio` verifies it and records the workspace as a profile (the first one registered becomes the default).

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

# Manage workspace profiles
slio profile list
slio profile use otherworkspace
```

Add `--format json` to any read command for structured output instead of Markdown. Add `--download` to `slio thread` to save attachments locally and print their paths. Run `slio --help` or `slio <command> --help` for the full flag reference.

### Multiple workspaces

Commands that take a URL (`slio thread`) pick the right profile automatically from the URL's host. Commands without one (`slio search`, `slio channel list`) use the default profile; pass `--profile <name>` to use another one, or `slio profile use <name>` to change the default.

### Environment variables

- `SLIO_TOKEN` overrides the stored token for a single invocation, bypassing profile resolution.
- `SLIO_PROFILE` selects a profile without passing `--profile` every time.
- `SLACK_TOKEN` is intentionally **not** read — it's a shared namespace other tools populate with bot tokens, which would cause confusing partial failures (e.g. only `search` failing, since `search.messages` requires a user token).

## Agent Skill

This repo ships an [Agent Skill](./skills/slio/SKILL.md) that tells an AI agent when and how to reach for `slio`. Install it by either:

- copying `skills/slio/` into your agent's skills directory (e.g. `~/.claude/skills/slio/` for Claude Code), or
- using a skill installer that consumes GitHub repos directly, e.g. [`npx skills`](https://www.npmjs.com/package/skills).

## Out of scope

`slio` never posts, reacts, or uploads — replies are drafted by the AI and posted by the human. Canvases and other non-message content, browser-session tokens (`xoxc`/`xoxd`), and OS keychain storage are also out of scope. See [the tracking issue](https://github.com/178inaba/slio/issues/1) for the full list.
