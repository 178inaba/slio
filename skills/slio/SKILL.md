---
name: slio
description: Read Slack threads, channel history, and search results directly from the CLI instead of via pasted screenshots. Use whenever the user shares a Slack link (message permalink, thread reply link) or asks to look up, search, or check recent activity in Slack.
---

# slio

`slio` is a read-only Slack CLI built for AI agents: it turns a thread URL, a channel, or a search query into AI-readable Markdown, so you can read Slack conversations directly instead of asking the user to paste screenshots.

## When to reach for it

- The user shares a Slack URL (a message permalink or a thread reply link) — run `slio thread <url>` to fetch the full thread.
- The user asks about recent activity in a channel — run `slio history <channel>` (accepts a URL, a channel ID, or `#name`).
- The user asks you to find something in Slack — run `slio search <query>`, passing Slack's own search syntax through unchanged.
- You need to know which channels are visible to the token — run `slio channel list`.

## Operating contract

- **Thread URLs**: pass the URL to `slio thread` exactly as given, including any `?thread_ts=...&cid=...` query string — both permalink forms resolve to the same thread.
- **Search syntax pass-through**: don't rewrite or simplify the user's query. Slack's own modifiers (`in:#channel`, `from:@user`, `after:`, `before:`, etc.) are passed straight through to Slack's search — use them instead of inventing your own filtering.
- **Drilling down**: `history`/`search` output includes a thread permalink next to any message with replies — follow up with `slio thread <permalink>` to read the full discussion.
- **Attachments**: files are shown as metadata (name/type/size) by default. Add `--download` (on `slio thread`) when you need to read an attachment's actual contents; it saves files to a local temp directory and prints their paths so you can read them.
- **Multiple workspaces**: `slio` auto-selects the right profile from a URL's host. For commands without a URL (`search`, `channel list`), pass `--profile <name>` if the user has more than one workspace registered and the default isn't the right one — check with `slio profile list`.
- **Structured output**: add `--format json` when you need to parse the result programmatically rather than read it as Markdown.
- **First-time setup**: if a command fails because no profile is registered, tell the user to run `slio auth login` (they'll need a Slack user token — see the repo README for how to get one).

Run `slio --help` or `slio <command> --help` for the full flag reference; it's the source of truth for exact flags and defaults, so this document doesn't duplicate it.
