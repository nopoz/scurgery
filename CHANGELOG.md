# Changelog

Notable changes to scurgery are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Exit codes are a contract that automation depends on. Any change to the
meaning of an exit code is a breaking change and is called out as one here.

## [Unreleased]

## [0.1.0] - 2026-08-22

First tagged release.

### Added

- `apply`, `remove`, `status` and `diff` subcommands for adding a named set of
  blocks to a tailnet policy file and removing exactly those blocks again.
- Removal that reproduces the original policy file byte for byte, including
  comment placement and trailing-comma style.
- `--dry-run`, `--yes`, `--force` and `--skip-conflicts` on the write paths.
- `--json` output for `status` and `diff`, with human-readable output on
  stderr so that stdout carries one document and nothing else.
- Structural matching for removing a bundle that was applied under a different
  name, via `remove <bundle.hujson> --match-structural`.
- Credentials from `TS_API_KEY` and `TS_TAILNET`, resolved from the command
  line, then the environment, then a config file under `$XDG_CONFIG_HOME`.
- A safety ladder on every write: conditional read, self-check,
  server-side validation, diff, local backup, confirmation, conditional write.
- `version` subcommand.

[Unreleased]: https://github.com/nopoz/scurgery/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/nopoz/scurgery/releases/tag/v0.1.0
