# Changelog

## [v1.0.6](https://github.com/178inaba/slio/compare/v1.0.5...v1.0.6) - 2026-09-02

### Changes
- Approve and auto-merge Dependabot pull requests by @178inaba in https://github.com/178inaba/slio/pull/69
### Dependency updates
- Bump golangci/golangci-lint from v2.13.1 to v2.13.2 by @dependabot[bot] in https://github.com/178inaba/slio/pull/71
- Bump github.com/slack-go/slack from 0.27.0 to 0.29.0 by @dependabot[bot] in https://github.com/178inaba/slio/pull/72

## [v1.0.5](https://github.com/178inaba/slio/compare/v1.0.4...v1.0.5) - 2026-09-02

### Changes
- Pin the npx skills install command to the stable tag by @178inaba in https://github.com/178inaba/slio/pull/58
- Correct the recorded reason for the npx skills URL form by @178inaba in https://github.com/178inaba/slio/pull/61
- Assert README's npx skills command is only ever the pinned URL by @178inaba in https://github.com/178inaba/slio/pull/63
- Take the release body from GitHub's generated notes by @178inaba in https://github.com/178inaba/slio/pull/66
- Derive the README install URL from the marketplace entry by @178inaba in https://github.com/178inaba/slio/pull/67

## [v1.0.4](https://github.com/178inaba/slio/compare/v1.0.3...v1.0.4) - 2026-08-31

- Enable revive and adopt the newer standard-library forms Go 1.27 offers by @178inaba in https://github.com/178inaba/slio/pull/54
- Assert the plugin distribution invariants in CI by @178inaba in https://github.com/178inaba/slio/pull/53
- Port the JSON handling to encoding/json/v2 by @178inaba in https://github.com/178inaba/slio/pull/57

## [v1.0.3](https://github.com/178inaba/slio/compare/v1.0.2...v1.0.3) - 2026-08-31

- Leave the attest-build-provenance recommendation to its own README by @178inaba in https://github.com/178inaba/slio/pull/51

## [v1.0.2](https://github.com/178inaba/slio/compare/v1.0.1...v1.0.2) - 2026-08-31

- Resolve the plugin through a stable tag carrying a tagpr-bumped version by @178inaba in https://github.com/178inaba/slio/pull/45

## [v1.0.1](https://github.com/178inaba/slio/compare/v1.0.0...v1.0.1) - 2026-08-31

- Attest the release archives with build provenance and move the App token to client-id by @178inaba in https://github.com/178inaba/slio/pull/42

## [v0.0.1](https://github.com/178inaba/slio/commits/v0.0.1) - 2026-08-31

- Implement the initial read-only Slack CLI (auth / profile / thread / history / search / channel list) by @178inaba in https://github.com/178inaba/slio/pull/2
- Mask the token input in auth login and move human-facing output to stderr by @178inaba in https://github.com/178inaba/slio/pull/4
- Add a GitHub Actions CI workflow (build/test/lint) and Dependabot by @178inaba in https://github.com/178inaba/slio/pull/6
- Build the cobra command tree from constructors by @178inaba in https://github.com/178inaba/slio/pull/11
- Align the Dependabot cooldown, the CI version-guard step, and the go directive with cflio by @178inaba in https://github.com/178inaba/slio/pull/12
- Align the runtime contract with cflio/rdsh: signals, duration --timeout, exit code 124, single error print, per-command --format by @178inaba in https://github.com/178inaba/slio/pull/14
- Add CLAUDE.md documenting the agent-facing contract and shared conventions by @178inaba in https://github.com/178inaba/slio/pull/16
- Make --format a pflag.Value type so cobra validates it during flag parsing by @178inaba in https://github.com/178inaba/slio/pull/17
- Make auth login's prompts cancellable so an interrupt does not save the profile anyway by @178inaba in https://github.com/178inaba/slio/pull/18
- Ignore .claude/worktrees so linked worktrees do not dirty git status by @178inaba in https://github.com/178inaba/slio/pull/21
- Ship the Agent Skill as a Claude Code plugin with a self-hosted marketplace by @178inaba in https://github.com/178inaba/slio/pull/22
- Catch the signal only while the terminal is modified, and die of it by @178inaba in https://github.com/178inaba/slio/pull/26
- End the run when the re-raise cannot kill, and read --timeout after parsing by @178inaba in https://github.com/178inaba/slio/pull/27
- Report a mistyped subcommand under a group command instead of exiting 0 by @178inaba in https://github.com/178inaba/slio/pull/28
- Record the mistyped-subcommand failure as an error instead of reporting it from the help function by @178inaba in https://github.com/178inaba/slio/pull/30
- Mark the message the permalink points at in slio thread output by @178inaba in https://github.com/178inaba/slio/pull/32
- Move the go directive to 1.27 and bump golangci-lint to v2.13.1 by @178inaba in https://github.com/178inaba/slio/pull/35
- Enable the gofmt formatter in .golangci.yml so CI fails on unformatted code by @178inaba in https://github.com/178inaba/slio/pull/36
- Release slio with tagpr and GoReleaser, and report the version through slio --version by @178inaba in https://github.com/178inaba/slio/pull/40
