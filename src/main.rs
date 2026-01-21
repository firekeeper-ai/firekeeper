mod agent;
mod cli;
mod config;

mod orchestrator;
mod rule;
mod worker;

use clap::Parser;
use cli::{Cli, Commands};
use config::Config;
use tracing::{error, info, trace};

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
            if std::path::Path::new(&args.config).exists() {
                error!("Error: {} already exists", args.config);
                std::process::exit(1);
            }

            std::fs::write(&args.config, config::default_config_template()).unwrap_or_else(|e| {
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

            let max_parallel_workers = args
                .max_parallel_workers
                .or(config.worker.max_parallel_workers);
            let base_url = args
                .base_url
                .as_deref()
                .or(Some(&config.llm.base_url))
                .unwrap();
            let model = args.model.as_deref().or(Some(&config.llm.model)).unwrap();

            orchestrator::orchestrate_and_run(
                &config.rules,
                &args.base,
                config.worker.max_files_per_task,
                max_parallel_workers,
                base_url,
                &args.api_key,
                model,
                args.dry_run,
                args.output.as_deref(),
            )
            .await;
        }
    }
}
