<h1 align="center">

<img src="images/banner.jpg" alt="firekeeper" width="670" />
<br>

_firekeeper_

</h1>

<span align="center">

_Agentic AI code reviewer CLI_

_Parallel review, custom rules, agent skills, run anywhere_

[![npm version](https://img.shields.io/npm/v/@firekeeper.ai/firekeeper)](https://www.npmjs.com/package/@firekeeper.ai/firekeeper)
[![GitHub release](https://img.shields.io/github/v/release/firekeeper-ai/firekeeper)](https://github.com/firekeeper-ai/firekeeper/releases)
[![License](https://img.shields.io/github/license/firekeeper-ai/firekeeper)](https://github.com/firekeeper-ai/firekeeper/blob/main/LICENSE)

</span>

## Features

- Customizable LLM configuration & bring your own API key for privacy
- Context engineering with files, shell commands, and Agent Skills
- Custom review range by commit, date, or entire repo
- Same tool for local dev, git hooks, and CI/CD
- Parallel review for speed and focus
- Structured output with `--output`
- Traceability with `--trace`

## Installation

<details open>

<summary>Shell (macOS/Linux)</summary>

```sh
curl --proto '=https' --tlsv1.2 -LsSf https://github.com/firekeeper-ai/firekeeper/releases/latest/download/firekeeper-installer.sh | sh
```

</details>

<details>

<summary>PowerShell (Windows)</summary>

```powershell
powershell -ExecutionPolicy Bypass -c "irm https://github.com/firekeeper-ai/firekeeper/releases/latest/download/firekeeper-installer.ps1 | iex"
```

</details>

<details>

<summary>npm</summary>

```sh
npm install -g @firekeeper.ai/firekeeper
```

</details>

## Getting Started

Init a config file `firekeeper.toml`:

```bash
firekeeper init
```

Set LLM API key (OpenRouter by default):

```bash
export FIREKEEPER_LLM_API_KEY=sk-xxxxxxxxxxxxxx
```

Review uncommitted changes or the last commit:

```bash
firekeeper review
```

<details>

<summary>More examples</summary>

> Review uncommitted changes only, suitable for git hooks or coding agent hooks:
>
> ```bash
> firekeeper review --base HEAD
> ```
>
> Review changes from 1 day ago with structured output, suitable for CI/CD pipelines:
>
> ```bash
> firekeeper review --base "@{1.day.ago}" --output /tmp/report.json --trace /tmp/trace.md
> ```
>
> Review all files (ensure you have sufficient LLM token budget):
>
> ```bash
> firekeeper review --base ROOT
> ```

</details>

## FAQ

### Why use a dedicated AI code reviewer instead of coding agents with MCP/Skills?

- **Cost efficiency**: Reviewers need less coding capability than code generators, so you can use cheaper models (Gemini Flash vs Pro, Claude Haiku vs Opus)
- **Integration**: CLI design fits naturally into git hooks and CI/CD pipelines
- **Specialized tooling**: Reviewer agents can have a different, optimized tool set
- **Performance at scale**: Parallel execution with filtered scopes keeps reviews fast and focused, preventing quality degradation on large codebases

### Why doesn't this tool fix bugs after review?

Fixing bugs requires high-quality output (passing compilation and tests), which coding agents already handle well. To avoid duplicate responsibility, firekeeper focuses solely on code review.

**Recommended workflow**: Integrate firekeeper in pre-commit git hooks → coding agent triggers the hook → sees review results → auto-optimizes the code.

### What should I review with this tool?

**Don't use for**: Issues caught by static analysis tools (formatters, linters, compilers, static analyzers). They're faster, more accurate, and cheaper.

**Do use for**: Semantic rules and conventions that traditional tools can't detect:

- Documentation updates after code changes
- Error logging after exception handling
- Code duplication that should be extracted into modules
- Project-specific conventions and patterns

This tool is designed for **_user-defined rules_**, not built-in nitpicking.

## [CHANGELOG](./CHANGELOG.md)
