mod cli;
mod config;
mod llm;
mod orchestrator;
mod rule;
mod suggest;
mod tool;
mod types;
mod util;
mod worker;

use clap::Parser;
use cli::{Cli, Commands};
use config::Config;
use toml_scaffold::TomlScaffold;
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
            if std::path::Path::new(&args.config).exists() && !args.r#override {
                error!("Error: {} already exists", args.config);
                std::process::exit(1);
            }

            std::fs::write(&args.config, config::Config::init().to_scaffold().unwrap())
                .unwrap_or_else(|e| {
                    error!("Error writing config: {}", e);
                    std::process::exit(1);
                });

            info!("Created {}", args.config);
        }
        Commands::Review(args) => {
            let mut config = Config::load(&args.config).unwrap_or_else(|e| {
                error!("Failed to load config: {}", e);
                std::process::exit(1);
            });

            if let Err(e) = config.apply_overrides(&args.config_overrides) {
                error!("Failed to apply config overrides: {}", e);
                std::process::exit(1);
            }

            trace!("args: {:#?}", args);
            trace!("config: {:#?}", config);

            orchestrator::orchestrate_and_run(
                &config.rules,
                &args.base,
                config.worker.max_files_per_task,
                config.worker.max_parallel_workers,
                &config.llm.base_url,
                &args.api_key,
                &config.llm.model,
                &config.llm.headers,
                &config.llm.body,
                args.dry_run,
                args.output.as_deref(),
                args.trace.as_deref(),
            )
            .await;
        }
        Commands::Suggest(args) => {
            let mut config = Config::load(&args.config).unwrap_or_else(|e| {
                error!("Failed to load config: {}", e);
                std::process::exit(1);
            });

            if let Err(e) = config.apply_overrides(&args.config_overrides) {
                error!("Failed to apply config overrides: {}", e);
                std::process::exit(1);
            }

            suggest::suggest(
                &args.base,
                &config,
                &args.api_key,
                &config.llm.base_url,
                &config.llm.model,
                args.output.as_deref(),
                args.trace.as_deref(),
            )
            .await
            .unwrap_or_else(|e| {
                error!("Failed to suggest rules: {}", e);
                std::process::exit(1);
            });
        }
    }
}
