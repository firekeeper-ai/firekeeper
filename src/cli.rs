use clap::{Parser, Subcommand};

// Display order for API key option (placed at top of help text)
const API_KEY_DISPLAY_ORDER: usize = 0;
// Display order for log level option (placed at end of help text)
const LOG_LEVEL_DISPLAY_ORDER: usize = 100;

/// CLI arguments
#[derive(Parser)]
#[command(name = "firekeeper", version, about = "Code review tool that enforces custom rules", long_about = None)]
pub struct Cli {
    /// Log level (see https://docs.rs/tracing-subscriber/latest/tracing_subscriber/filter/struct.EnvFilter.html)
    /// [env: FIREKEEPER_LOG=] [default: info]
    #[arg(
        long,
        env = "FIREKEEPER_LOG",
        default_value = "info",
        global = true,
        hide_default_value = true,
        hide_env = true,
        display_order = LOG_LEVEL_DISPLAY_ORDER,
        verbatim_doc_comment
    )]
    pub log_level: String,

    #[command(subcommand)]
    pub command: Commands,
}

/// CLI subcommands
#[derive(Subcommand)]
pub enum Commands {
    /// Initialize a default firekeeper.toml config file
    Init(InitArgs),
    /// Review code changes against rules
    Review(ReviewArgs),
    /// Suggest new rules based on code changes
    Suggest(SuggestArgs),
}

/// Arguments for the init command
#[derive(Parser)]
pub struct InitArgs {
    /// Path to config file
    #[arg(long, default_value = "firekeeper.toml")]
    pub config: String,

    /// Override existing config file
    #[arg(long)]
    pub r#override: bool,
}

/// Arguments for the review command
#[derive(Parser, Debug)]
pub struct ReviewArgs {
    /// Base commit to compare against.
    /// Examples: HEAD^ or ^, HEAD~1 or ~1, commit hash, @{1.day.ago}.
    /// HEAD for uncommitted changes, ROOT for all files
    /// [default: HEAD if uncommitted changes exist, otherwise ^]
    #[arg(
        long,
        default_value = "",
        hide_default_value = true,
        verbatim_doc_comment
    )]
    pub base: String,

    /// Path to config file (initialize with `firekeeper init`)
    #[arg(long, default_value = "firekeeper.toml")]
    pub config: String,

    /// Override config values using dot notation (e.g. llm.model=gpt-4)
    #[arg(long = "config-override")]
    pub config_overrides: Vec<String>,

    /// LLM API key
    #[arg(long, env = "FIREKEEPER_LLM_API_KEY", display_order = API_KEY_DISPLAY_ORDER)]
    pub api_key: String,

    /// Dry run: only show tasks without executing workers
    #[arg(long)]
    pub dry_run: bool,

    /// Output file path (.md or .json)
    #[arg(long)]
    pub output: Option<String>,

    /// Trace file path to record agent responses and tool use (.md or .json)
    #[arg(long)]
    pub trace: Option<String>,
}

/// Arguments for the suggest command
#[derive(Parser, Debug, Clone)]
pub struct SuggestArgs {
    /// Base commit to compare against.
    /// Examples: HEAD^ or ^, HEAD~1 or ~1, commit hash, @{1.day.ago}.
    /// HEAD for uncommitted changes, ROOT for all files
    /// [default: HEAD if uncommitted changes exist, otherwise ^]
    #[arg(
        long,
        default_value = "",
        hide_default_value = true,
        verbatim_doc_comment
    )]
    pub base: String,

    /// Path to config file to read existing rules
    #[arg(long, default_value = "firekeeper.toml")]
    pub config: String,

    /// Override config values using dot notation (e.g. llm.model=gpt-4)
    #[arg(long = "config-override")]
    pub config_overrides: Vec<String>,

    /// LLM API key
    #[arg(long, env = "FIREKEEPER_LLM_API_KEY", display_order = API_KEY_DISPLAY_ORDER)]
    pub api_key: String,

    /// Output file path (.md or .json)
    #[arg(long)]
    pub output: Option<String>,

    /// Trace file path to record agent responses and tool use (.md or .json)
    #[arg(long)]
    pub trace: Option<String>,
}
