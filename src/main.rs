mod agent;
mod cli;
mod config;
mod rule;

use clap::Parser;
use cli::{Cli, Commands};
use config::Config;

#[tokio::main]
async fn main() {
    let cli = Cli::parse();
    
    match &cli.command {
        Commands::Review(args) => {
            let config = Config::load(&args.config).unwrap_or_else(|e| {
                eprintln!("Failed to load config: {}", e);
                std::process::exit(1);
            });
            
            println!("diff: {}", args.diff);
            println!("config: {}", args.config);
            println!("base_url: {}", config.llm.base_url);
            println!("model: {}", config.llm.model);
            println!("api_key: {}...", &args.api_key[..args.api_key.len().min(8)]);
            println!("rules loaded: {}", config.rules.len());
        }
    }
}
