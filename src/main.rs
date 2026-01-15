mod cli;
mod config;
mod rule;

use clap::Parser;
use cli::{Cli, Commands};
use config::Config;

fn main() {
    let cli = Cli::parse();
    
    match &cli.command {
        Commands::Review(args) => {
            let config = Config::load(&args.config).unwrap_or_else(|e| {
                eprintln!("Failed to load config: {}", e);
                std::process::exit(1);
            });
            
            println!("diff: {}", args.diff);
            println!("config: {}", args.config);
            println!("rules loaded: {}", config.rules.len());
        }
    }
}
