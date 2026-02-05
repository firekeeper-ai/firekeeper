# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[unreleased]: https://github.com/firekeeper-ai/firekeeper/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/firekeeper-ai/firekeeper/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/firekeeper-ai/firekeeper/releases/tag/v0.1.0
