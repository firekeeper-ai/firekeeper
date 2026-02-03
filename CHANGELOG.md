# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- OpenRouter-specific headers (X-Title, HTTP-Referer) in default LLM config
- Parallel tool calls support in default LLM config
- General-purpose rule factory methods: `no_code_duplication()`, `no_magic_numbers()`, `no_hardcoded_credentials()`
- Exception for factory methods and templates in No Code Duplication rule

### Changed

- Renamed `WorkerConfig` to `ReviewConfig`
- Renamed config field `worker` to `review`
- Default config now uses three general-purpose rules instead of JS/TS-specific example

### Fixed

- Exit with failure code (1) when workers fail or abort, not just when violations are found

## [0.1.0] - 2026-02-03

### Added

- Initial release with `review` and `suggest` commands
- LLM-powered code review against custom rules
- Support for OpenRouter API
- Git integration for reviewing changes
- Configuration via `firekeeper.toml`
- Multiple output formats (markdown, JSON)
- Trace logging for debugging

[unreleased]: https://github.com/firekeeper-ai/firekeeper/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/firekeeper-ai/firekeeper/releases/tag/v0.1.0
