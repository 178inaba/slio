# CLAUDE.md

## Agent contract

slio's primary consumer is an AI coding agent calling it from a shell; the conventions below exist to keep that contract stable.

- **Three-way documentation sync** — user-facing CLI behaviour is stated in `README.md`, `skills/slio/SKILL.md`, and the cobra help strings under `internal/cmd/`. Changing the CLI means updating all three in the same PR. Overlaps today, each stated in more than one of the three: the default `md` output format, profile and environment-variable resolution, the 90 s default `--timeout` with its exit codes, `--download` on `slio thread`, and the linked-message marker `slio thread` puts on the message its URL points at.
- **The plugin description is a synced surface** — the one-line description in `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json` (top level and the `plugins[]` entry), and `.goreleaser.yaml` (the `homebrew_casks` entry's `description`, which becomes the cask's `desc`) must be byte-identical, and must say the same thing as the repository's GitHub "About" text and the README's opening line.
- **Output format, stream split, and exit codes are a contract** — `--format md|json`, the stdout/stderr split (data on stdout, human-facing messages on stderr — established in 178inaba/slio#3), and the exit codes produced by `classifyFailure` in `internal/cmd/root.go` (`0`, `124` when the `--timeout` deadline expires, following the GNU `timeout` convention, `1` for everything else) are branched on mechanically by agent consumers. An interrupt produces no code at all: slio terminates by the signal, so a parent sees `WIFSIGNALED` rather than a normal exit. Changing any of them is a breaking change, held to a higher bar than for a human-facing CLI.
- **`skills/slio/SKILL.md` is router-style** — activation conditions and workflow only; per-command syntax stays delegated to `slio --help`.

## Code conventions

Shared across the sibling CLIs (cflio, rdsh, slio):

- cobra commands are wired constructor-style (`newXCmd()` returning `*cobra.Command`); no package-level command or flag variables.
- Output formatting lives in `internal/format`. An API-client package is named after the service, and its main file after the package. slio's client package is `slackclient` rather than `slack` — intentional, to avoid colliding with the `slack-go/slack` dependency import.
