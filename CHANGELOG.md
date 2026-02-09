# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `config` command with `format` and `validate` subcommands for config file operations
- `config format` re-renders TOML config with schema-derived comments
- `config validate` checks TOML config syntax and structure

### Changed

- Upgraded `toml-scaffold` dependency from 0.1 to 0.2
- Added documentation and AI agent note to `Config` struct

## [0.3.0] - 2026-02-07

### Added

- Version field to JSON output schemas (`TraceFile` and `ViolationFile`)
- Version compatibility check in `render` command (checks minor version)
- Timestamp and elapsed time display for each message in trace markdown output
- `chrono` dependency for timestamp formatting

### Changed

- **BREAKING**: Trace JSON schema now uses `TraceFile` wrapper with `version` and `entries` fields
- **BREAKING**: Trace JSON schema now uses `rule` object (containing full `RuleBody`) instead of separate `rule_name` and `rule_instruction` fields in `TraceEntry`
- **BREAKING**: Violation JSON schema now uses `ViolationFile` wrapper with `version`, `violations`, and `tips` fields
- **BREAKING**: Trace JSON schema now stores messages as `Vec<TimedMessage>` and tools as `Vec<ToolDefinition>` instead of generic types
- Upgraded `tiny-loop` dependency from 0.3 to 0.4

## [0.2.2] - 2026-02-07

### Added

- `firekeeper render` command to convert JSON trace/output files to Markdown format
  - Supports both trace and output JSON formats
  - Optional `--output` flag (prints to stdout if omitted)
- SIGTERM signal handling for graceful shutdown on Unix systems (in addition to existing SIGINT/Ctrl+C support)
- Early stop optimization in agent loop:
  - Stops immediately when report tool is called with 0 violations
  - Detects duplicated tool calls to prevent dead loops (returns error)

## [0.2.1] - 2026-02-05

### Changed

- Improved rule instruction format with structured Steps, Violation criteria, and Exemptions sections
- Enhanced firekeeper.toml with inline comments for better self-documentation
- Updated Documentation Sync rule to check code comments and prioritize CHANGELOG updates
- Made builtin rules more generic and language-agnostic

## [0.2.0] - 2026-02-03

### Added

- `resources` field in `ReviewConfig` and `RuleBody` to include additional context in reviews
  - `file://glob` - Load files matching glob pattern (e.g., `file://**/*.md`)
  - `skill://glob` - Load markdown files and extract frontmatter (title and description)
  - `sh://command` - Execute shell command and include output (uses `cmd /C` on Windows)
  - Global resources in `ReviewConfig` (default: `["file://README.md"]`)
  - Per-rule resources that are merged with global resources
  - Automatic deduplication of loaded files
- `exclude` field in rule configuration to filter files using glob patterns on top of `scope` patterns
- OpenRouter-specific headers (X-Title, HTTP-Referer) in default LLM config
- Parallel tool calls support in default LLM config
- General-purpose rule factory methods: `no_code_duplication()`, `no_magic_numbers()`, `no_hardcoded_credentials()`
- Exception for factory methods and templates in No Code Duplication rule

### Changed

- Renamed `WorkerConfig` to `ReviewConfig`
- Renamed config field `worker` to `review`
- Default config now uses three general-purpose rules instead of JS/TS-specific example
- Trace output now wraps system messages, user messages, and rule instructions in markdown code blocks for better readability

### Fixed

- Resource loading with `file://` now correctly matches relative paths like `README.md` and `src/*.rs` by stripping `./` prefix during glob matching
- Exit with failure code (1) when workers fail or abort, not just when violations are found

### Removed

- `suggest` command and related functionality
- Lua tool (disabled, kept in codebase for reference)

## [0.1.0] - 2026-02-03

### Added

- Initial release with `review` and `suggest` commands
- LLM-powered code review against custom rules
- Support for OpenRouter API
- Git integration for reviewing changes
- Configuration via `firekeeper.toml`
- Multiple output formats (markdown, JSON)
- Trace logging for debugging

[unreleased]: https://github.com/firekeeper-ai/firekeeper/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/firekeeper-ai/firekeeper/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/firekeeper-ai/firekeeper/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/firekeeper-ai/firekeeper/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/firekeeper-ai/firekeeper/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/firekeeper-ai/firekeeper/releases/tag/v0.1.0
