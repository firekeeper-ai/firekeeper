# Firekeeper

## Usage

### Full CLI Usage

<details>

<summary><code>firekeeper review --help</code></summary>

```sh
Review code changes against rules

Usage: firekeeper review [OPTIONS] --api-key <API_KEY>

Options:
      --base <BASE>
          Base commit to compare against. Examples: HEAD^ or ^, HEAD~1 or ~1, commit hash, @{1.day.ago}, HEAD for uncommitted changes, ROOT for all files [default: HEAD if uncommitted changes exist, otherwise ^]
      --log-level <LOG_LEVEL>
          Log level (see https://docs.rs/tracing-subscriber/latest/tracing_subscriber/filter/struct.EnvFilter.html) [env: FIREKEEPER_LOG=] [default: info]
      --config <CONFIG>
          Path to config file (initialize with `firekeeper init`) [default: firekeeper.toml]
      --api-key <API_KEY>
          OpenRouter API key [env: OPENAI_API_KEY=]
      --dry-run
          Dry run: only show tasks without executing workers
      --max-parallel-workers <MAX_PARALLEL_WORKERS>
          Maximum number of parallel workers (defaults to unlimited)
  -h, --help
          Print help
```

</details>