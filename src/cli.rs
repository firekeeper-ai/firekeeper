use clap::{Parser, Subcommand};
use crate::config::{DEFAULT_BASE_URL, DEFAULT_MODEL};

#[derive(Parser)]
#[command(name = "firekeeper", version, about = "Code review tool that enforces custom rules", long_about = None)]
pub struct Cli {
    /// Log level (see https://docs.rs/tracing-subscriber/latest/tracing_subscriber/filter/struct.EnvFilter.html)
    /// [env: FIREKEEPER_LOG=] [default: info]
    #[arg(long, env = "FIREKEEPER_LOG", default_value = "info", global = true, hide_default_value = true, hide_env = true, verbatim_doc_comment)]
    pub log_level: String,
    
    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Subcommand)]
pub enum Commands {
    /// Initialize a default firekeeper.toml config file
    Init(InitArgs),
    /// Review code changes against rules
    Review(ReviewArgs),
}

#[derive(Parser)]
pub struct InitArgs {
    /// Path to config file
    #[arg(long, default_value = "firekeeper.toml")]
    pub config: String,
}

#[derive(Parser, Debug)]
pub struct ReviewArgs {
    /// Base commit to compare against.
    /// Examples: HEAD^ or ^, HEAD~1 or ~1, commit hash, @{1.day.ago}.
    /// HEAD for uncommitted changes, ROOT for all files
    /// [default: HEAD if uncommitted changes exist, otherwise ^]
    #[arg(long, default_value = "", hide_default_value = true, verbatim_doc_comment)]
    pub base: String,
    
    /// Path to config file (initialize with `firekeeper init`)
    #[arg(long, default_value = "firekeeper.toml")]
    pub config: String,
    
    /// LLM API key
    #[arg(long, env = "FIREKEEPER_LLM_API_KEY")]
    pub api_key: String,
    
    /// LLM base URL
    #[arg(long, default_value = DEFAULT_BASE_URL)]
    pub base_url: Option<String>,
    
    /// LLM model
    #[arg(long, default_value = DEFAULT_MODEL)]
    pub model: Option<String>,
    
    /// Dry run: only show tasks without executing workers
    #[arg(long)]
    pub dry_run: bool,
    
    /// Maximum number of parallel workers (defaults to unlimited)
    #[arg(long)]
    pub max_parallel_workers: Option<usize>,
}
