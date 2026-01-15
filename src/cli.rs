use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "firekeeper", version, about = "Code review tool that enforces custom rules", long_about = None)]
pub struct Cli {
    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Subcommand)]
pub enum Commands {
    /// Review code changes against rules
    Review(ReviewArgs),
}

#[derive(Parser)]
pub struct ReviewArgs {
    /// Git diff range (e.g. HEAD~1..HEAD)
    #[arg(long, default_value = "HEAD~1..HEAD")]
    pub diff: String,
    
    /// Path to config file (JSON/TOML)
    #[arg(long, default_value = "firekeeper.toml")]
    pub config: String,
    
    /// OpenRouter API key
    #[arg(long, env = "OPENAI_API_KEY")]
    pub api_key: String,
}
