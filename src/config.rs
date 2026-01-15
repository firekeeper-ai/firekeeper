use serde::Deserialize;
use std::fs;

#[derive(Deserialize)]
pub struct Config {
    pub llm: LlmConfig,
    pub rules: Vec<crate::rule::body::RuleBody>,
}

#[derive(Deserialize)]
pub struct LlmConfig {
    pub base_url: String,
    pub model: String,
}

impl Config {
    pub fn load(path: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let content = fs::read_to_string(path)?;
        let config = toml::from_str(&content)?;
        Ok(config)
    }
}
