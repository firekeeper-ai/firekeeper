mod cli;
mod config;
mod llm;
mod review;
mod rule;
mod tool;
mod types;
mod util;

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

            std::fs::write(
                &args.config,
                config::Config::default().to_scaffold().unwrap(),
            )
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

            review::orchestrator::orchestrate_and_run(
                &config.rules,
                &args.base,
                config.review.max_files_per_task,
                config.review.max_parallel_workers,
                &config.llm.base_url,
                &args.api_key,
                &config.llm.model,
                &config.llm.headers,
                &config.llm.body,
                args.dry_run,
                args.output.as_deref(),
                args.trace.as_deref(),
                &args.config,
                &config.review.resources,
                &config.review.allowed_shell_commands,
            )
            .await;
        }
        Commands::Render(args) => {
            let content = std::fs::read_to_string(&args.input).unwrap_or_else(|e| {
                error!("Failed to read input file: {}", e);
                std::process::exit(1);
            });

            let markdown = if let Ok(trace_file) =
                serde_json::from_str::<review::render::TraceFile>(&content)
            {
                review::render::format_trace_markdown(&trace_file.entries)
            } else if let Ok(violation_file) =
                serde_json::from_str::<review::render::ViolationFile>(&content)
            {
                review::render::format_violations(&violation_file.violations, &violation_file.tips)
            } else {
                // Check version compatibility
                if let Ok(value) = serde_json::from_str::<serde_json::Value>(&content) {
                    if let Some(file_version) = value.get("version").and_then(|v| v.as_str()) {
                        let current_version = env!("CARGO_PKG_VERSION");
                        let file_minor = file_version.split('.').nth(1);
                        let current_minor = current_version.split('.').nth(1);
                        if file_minor != current_minor {
                            error!(
                                "Incompatible file version: {} (current: {})",
                                file_version, current_version
                            );
                            std::process::exit(1);
                        }
                    }
                }
                error!("Invalid JSON format");
                std::process::exit(1);
            };

            if let Some(output_path) = &args.output {
                std::fs::write(output_path, markdown).unwrap_or_else(|e| {
                    error!("Failed to write output file: {}", e);
                    std::process::exit(1);
                });
                info!("Rendered to {}", output_path);
            } else {
                println!("{}", markdown);
            }
        }
        Commands::Config(args) => match &args.command {
            cli::ConfigCommands::Format => {
                let content = std::fs::read_to_string(&args.config).unwrap_or_else(|e| {
                    error!("Failed to read config file: {}", e);
                    std::process::exit(1);
                });

                let config: config::Config = toml::from_str(&content).unwrap_or_else(|e| {
                    error!("Failed to parse TOML: {}", e);
                    std::process::exit(1);
                });

                let output = config.to_scaffold().unwrap_or_else(|e| {
                    error!("Failed to format TOML: {}", e);
                    std::process::exit(1);
                });

                std::fs::write(&args.config, output).unwrap_or_else(|e| {
                    error!("Failed to write config file: {}", e);
                    std::process::exit(1);
                });

                info!("Formatted {}", args.config);
            }
            cli::ConfigCommands::Validate => {
                let content = std::fs::read_to_string(&args.config).unwrap_or_else(|e| {
                    error!("Failed to read config file: {}", e);
                    std::process::exit(1);
                });

                match toml::from_str::<config::Config>(&content) {
                    Ok(_) => {
                        info!("Config is valid: {}", args.config);
                    }
                    Err(e) => {
                        error!("Invalid config: {}", e);
                        std::process::exit(1);
                    }
                }
            }
        },
    }
}
