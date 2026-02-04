<h1 align="center">

<img src="images/banner.jpg" alt="firekeeper" width="670" />

<span style="font-family: 'Cinzel', serif; color: rgb(212 197 161); text-shadow: 0 0 10px rgba(212, 197, 161, 0.5)">
FIREKEEPER.AI
</span>

</h1>

<span align="center">

<p style="font-family: ui-serif, Georgia, Cambria, 'Times New Roman', Times, serif; color: rgb(212 197 161); font-size: 14px; font-style: italic; text-shadow: 0 0 10px rgba(212, 197, 161, 0.5)">
"Ashen one, hearken my words. Your codebase withers in the shadow of debt. Let the Firekeeper purify your craft and keep the flame of innovation alive."
</p>

[![npm version](https://img.shields.io/npm/v/@firekeeper.ai/firekeeper)](https://www.npmjs.com/package/@firekeeper.ai/firekeeper)
[![GitHub release](https://img.shields.io/github/v/release/firekeeper-ai/firekeeper)](https://github.com/firekeeper-ai/firekeeper/releases)
[![License](https://img.shields.io/github/license/firekeeper-ai/firekeeper)](https://github.com/firekeeper-ai/firekeeper/blob/main/LICENSE)

</span>

## Features

- Customizable LLM model/header/body & bring your own API key
- Custom review range by commit, date, or entire repo
- Run locally, in git hooks, or CI/CD with the same tool
- Auto task splitting and parallel review for speed and focus
- Inject additional context from files, shell commands, AGENTS.md, and Agent Skills
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

Review uncommitted changes or the last commit, suitable for git hooks or agent hooks:

```bash
firekeeper review
```

<details>

<summary><code>More examples</code></summary>

Review changes from 1 day ago with structured output, suitable for CI/CD pipelines:

```bash
firekeeper review --base "@{1.day.ago}" --output /tmp/report.json --trace /tmp/trace.md
```

Review all files (ensure you have sufficient LLM token budget):

```bash
firekeeper review --base ROOT
```

</details>

## [CHANGELOG](./CHANGELOG.md)
