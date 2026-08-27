# Changelog

Notable changes to scurgery are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Exit codes are a contract that automation depends on. Any change to the
meaning of an exit code is a breaking change and is called out as one here.

## [Unreleased]

## [0.2.0] - 2026-08-26

### Fixed

- `remove` no longer deletes a comment the operator wrote next to their own
  rule. The comments and whitespace attached to a member begin at the previous
  member's comma, so a comment added after an apply sits in front of scurgery's
  marker rather than after it, and removing the member took it too. Removal now
  takes the marker's own line and leaves everything before it where it was.
- The self-check on `remove` refuses a removal that loses such a comment. It
  could not see one before, because the comment belongs to a member that the
  removal is supposed to delete, so no comparison of the surviving members
  reaches it.
- `remove <bundle.hujson>` no longer reports a runtime error when the bundle is
  not installed at all. It pointed the operator at `--match-structural`, a run
  it had already worked out would remove nothing, while `remove <name>` treated
  the same situation as no change needed. Both forms now say what is installed
  instead and succeed, so a teardown that names the file it applied can be run
  more than once. The refusal is unchanged when content matching would in fact
  remove something, which is the case that flag exists for.
- `remove --match-structural` no longer warns that a container is now empty
  when it was already empty before the removal. The warning ends by inviting
  the operator to delete the container by hand, so raising it for one scurgery
  never touched points them at their own content.
- Asking a subcommand for help is no longer reported as a usage error.
  `scurgery apply --help` printed a bare flag list to stderr and exited 2,
  which is the code for a command line that was wrong. It now prints the usage
  text and the flag descriptions to stdout and exits 0, the same as
  `scurgery help`. A flag that does not exist is still a usage error, and now
  carries the usage text with it rather than the flag list alone.

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

[Unreleased]: https://github.com/nopoz/scurgery/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/nopoz/scurgery/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/nopoz/scurgery/releases/tag/v0.1.0
