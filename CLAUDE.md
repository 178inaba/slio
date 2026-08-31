# CLAUDE.md

## Agent contract

slio's primary consumer is an AI coding agent calling it from a shell; the conventions below exist to keep that contract stable.

- **Three-way documentation sync** — user-facing CLI behaviour is stated in `README.md`, `skills/slio/SKILL.md`, and the cobra help strings under `internal/cmd/`. Changing the CLI means updating all three in the same PR. Overlaps today, each stated in more than one of the three: the default `md` output format, profile and environment-variable resolution, the 90 s default `--timeout` with its exit codes, `--download` on `slio thread`, the linked-message marker `slio thread` puts on the message its URL points at, and `--version`.
- **The plugin description is a synced surface** — the one-line description in `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json` (top level and the `plugins[]` entry), and `.goreleaser.yaml` (the `homebrew_casks` entry's `description`, which becomes the cask's `desc`) must be byte-identical, and must say the same thing as the repository's GitHub "About" text and the README's opening line.
- **Output format, stream split, and exit codes are a contract** — `--format md|json`, the stdout/stderr split (data on stdout, human-facing messages on stderr — established in 178inaba/slio#3), and the exit codes produced by `classifyFailure` in `internal/cmd/root.go` (`0`, `124` when the `--timeout` deadline expires, following the GNU `timeout` convention, `1` for everything else) are branched on mechanically by agent consumers. An interrupt produces no code at all: slio terminates by the signal, so a parent sees `WIFSIGNALED` rather than a normal exit. Changing any of them is a breaking change, held to a higher bar than for a human-facing CLI.
- **`stable` is moved only by the release workflow** — the `plugins[]` entry in `.claude-plugin/marketplace.json` resolves through the `stable` tag, and `.github/workflows/release.yml` force-pushes it onto the new tag once the archives are published and attested. Moving it by hand ships content without changing the `version` in the manifest, and Claude Code keeps the copy it has cached, so updates stop silently.
- **The plugin's `version` lives in `.claude-plugin/plugin.json` only** — never on the `marketplace.json` entry. Claude Code always takes the manifest's value without warning, so a version in both places lets a stale manifest mask the other one. Keeping it in the manifest also puts it inside the tree `stable` points at, so the version a plugin advertises and the content it serves cannot disagree. tagpr bumps it through `versionFile` in `.tagpr`.
- **`skills/slio/SKILL.md` is router-style** — activation conditions and workflow only; per-command syntax stays delegated to `slio --help`.

## Code conventions

Shared across the sibling CLIs (cflio, rdsh, slio):

- cobra commands are wired constructor-style (`newXCmd()` returning `*cobra.Command`); no package-level command or flag variables.
- Output formatting lives in `internal/format`. An API-client package is named after the service, and its main file after the package. slio's client package is `slackclient` rather than `slack` — intentional, to avoid colliding with the `slack-go/slack` dependency import.
