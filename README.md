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
          Base commit to compare against (HEAD is prepended to ~ or ^, e.g. ~1, ^, commit hash) [default: ^]
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