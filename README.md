# Firekeeper

[![npm version](https://img.shields.io/npm/v/@firekeeper.ai/firekeeper)](https://www.npmjs.com/package/@firekeeper.ai/firekeeper)
[![GitHub release](https://img.shields.io/github/v/release/firekeeper-ai/firekeeper)](https://github.com/firekeeper-ai/firekeeper/releases)
[![License](https://img.shields.io/github/license/firekeeper-ai/firekeeper)](https://github.com/firekeeper-ai/firekeeper/blob/main/LICENSE)

Firekeeper reviews your current working state against a base commit using custom rules.

## Usage

### Examples

Init a config file `firekeeper.toml`:

```bash
firekeeper init
```

Set OpenRouter API key:

```bash
export FIREKEEPER_LLM_API_KEY=sk-xxxxxxxxxxxxxx
```

Review uncommitted changes or the last commit, suitable for git hooks or agent hooks:

```bash
firekeeper review
```

Review changes from 1 day ago, suitable for CI/CD pipelines:

```bash
firekeeper review --base "@{1.day.ago}"
```

Review all files (ensure you have sufficient LLM token budget):

```bash
firekeeper review --base ROOT
```

### Full CLI Usage

<details>

<summary><code>firekeeper review --help</code></summary>

```sh
Review code changes against rules

Usage: firekeeper review [OPTIONS] --api-key <API_KEY>

Options:
      --api-key <API_KEY>
          LLM API key [env: FIREKEEPER_LLM_API_KEY=]
      --base <BASE>
          Base commit to compare against.
          Examples: HEAD^ or ^, HEAD~1 or ~1, commit hash, @{1.day.ago}.
          HEAD for uncommitted changes, ROOT for all files
          [default: HEAD if uncommitted changes exist, otherwise ^]
      --config <CONFIG>
          Path to config file (initialize with `firekeeper init`) [default: firekeeper.toml]
      --config-override <CONFIG_OVERRIDES>
          Override config values using dot notation (e.g. llm.model=gpt-4)
      --dry-run
          Dry run: only show tasks without executing workers
      --output <OUTPUT>
          Output file path (.md or .json)
      --trace <TRACE>
          Trace file path to record agent responses and tool use (.md or .json)
      --log-level <LOG_LEVEL>
          Log level (see https://docs.rs/tracing-subscriber/latest/tracing_subscriber/filter/struct.EnvFilter.html)
          [env: FIREKEEPER_LOG=] [default: info]
  -h, --help
          Print help
```

</details>

<details>

<summary><code>firekeeper suggest --help</code></summary>

```sh
Usage: firekeeper suggest [OPTIONS] --api-key <API_KEY>

Options:
      --api-key <API_KEY>
          LLM API key [env: FIREKEEPER_LLM_API_KEY=]
      --base <BASE>
          Base commit to compare against.
          Examples: HEAD^ or ^, HEAD~1 or ~1, commit hash, @{1.day.ago}.
          HEAD for uncommitted changes, ROOT for all files
          [default: HEAD if uncommitted changes exist, otherwise ^]
      --config <CONFIG>
          Path to config file to read existing rules [default: firekeeper.toml]
      --config-override <CONFIG_OVERRIDES>
          Override config values using dot notation (e.g. llm.model=gpt-4)
      --output <OUTPUT>
          Output file path (.md or .json)
      --trace <TRACE>
          Trace file path to record agent responses and tool use (.md or .json)
      --log-level <LOG_LEVEL>
          Log level (see https://docs.rs/tracing-subscriber/latest/tracing_subscriber/filter/struct.EnvFilter.html)
          [env: FIREKEEPER_LOG=] [default: info]
  -h, --help
          Print help
```

</details>

## [CHANGELOG](./CHANGELOG.md)
