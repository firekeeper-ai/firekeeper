# Firekeeper

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
      --base-url <BASE_URL>
          LLM base URL [default: https://openrouter.ai/api/v1]
      --model <MODEL>
          LLM model [default: google/gemini-3-flash-preview]
      --dry-run
          Dry run: only show tasks without executing workers
      --max-parallel-workers <MAX_PARALLEL_WORKERS>
          Maximum number of parallel workers (defaults to unlimited)
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
