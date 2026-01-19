use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "firekeeper", version, about = "Code review tool that enforces custom rules", long_about = None)]
pub struct Cli {
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

#[derive(Parser)]
pub struct ReviewArgs {
    /// Base commit to compare against.
    /// E.g. ~1, ^, commit hash, or HEAD for uncommitted changes
    /// [default: HEAD if uncommitted changes exist, otherwise ^]
    #[arg(long, default_value = "", hide_default_value = true)]
    pub base: String,
    
    /// Path to config file (initialize with `firekeeper init`)
    #[arg(long, default_value = "firekeeper.toml")]
    pub config: String,
    
    /// OpenRouter API key
    #[arg(long, env = "OPENAI_API_KEY")]
    pub api_key: String,
    
    /// Dry run: only show tasks without executing workers
    #[arg(long)]
    pub dry_run: bool,
    
    /// Maximum number of parallel workers (defaults to unlimited)
    #[arg(long)]
    pub max_parallel_workers: Option<usize>,
}
