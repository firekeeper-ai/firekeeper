mod agent;
mod cli;
mod config;

mod orchestrator;
mod rule;
mod worker;

use clap::Parser;
use cli::{Cli, Commands};
use config::Config;
use tracing::{trace, error, info};

#[tokio::main]
async fn main() {
    let cli = Cli::parse();
    
    // Initialize tracing subscriber with log level from CLI/env
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::new(&cli.log_level))
        .without_time()
        .with_target(false)
        .init();
    
    match &cli.command {
        Commands::Init(args) => {
            const DEFAULT_CONFIG: &str = r#"[llm]
base_url = "https://openrouter.ai/api/v1"
model = "google/gemini-3-flash-preview"

[worker]
# Maximum number of files to process per task (optional, defaults to 5)
max_files_per_task = 5
# Maximum number of parallel workers (optional, defaults to unlimited)
# max_parallel_workers = 10

[[rules]]
# Name of the rule (required)
name = ""
# Brief description of the rule (optional, defaults to empty string)
description = ""
# Detailed instructions for the LLM on how to check this rule (required)
instruction = """
"""
# Glob patterns to match files this rule applies to (optional, defaults to ["**/*"])
scope = ["**/*"]
"#;
            
            if std::path::Path::new(&args.config).exists() {
                error!("Error: {} already exists", args.config);
                std::process::exit(1);
            }
            
            std::fs::write(&args.config, DEFAULT_CONFIG).unwrap_or_else(|e| {
                error!("Error writing config: {}", e);
                std::process::exit(1);
            });
            
            info!("Created {}", args.config);
        }
        Commands::Review(args) => {
            let config = Config::load(&args.config).unwrap_or_else(|e| {
                error!("Failed to load config: {}", e);
                std::process::exit(1);
            });
            
            trace!("args: {:#?}", args);
            trace!("config: {:#?}", config);
            
            let max_parallel_workers = args.max_parallel_workers.or(config.worker.max_parallel_workers);
            
            orchestrator::orchestrate_and_run(
                &config.rules,
                &args.base,
                config.worker.max_files_per_task,
                max_parallel_workers,
                &config.llm.base_url,
                &args.api_key,
                &config.llm.model,
                args.dry_run,
            ).await;
        }
    }
}
