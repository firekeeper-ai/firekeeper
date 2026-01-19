use serde::Deserialize;
use std::fs;

pub const DEFAULT_BASE_URL: &str = "https://openrouter.ai/api/v1";
pub const DEFAULT_MODEL: &str = "google/gemini-3-flash-preview";

#[derive(Deserialize, Debug)]
pub struct Config {
    pub llm: LlmConfig,
    #[serde(default)]
    pub worker: WorkerConfig,
    pub rules: Vec<crate::rule::body::RuleBody>,
}

#[derive(Deserialize, Debug)]
pub struct LlmConfig {
    #[serde(default = "default_base_url")]
    pub base_url: String,
    #[serde(default = "default_model")]
    pub model: String,
}

fn default_base_url() -> String {
    DEFAULT_BASE_URL.to_string()
}

fn default_model() -> String {
    DEFAULT_MODEL.to_string()
}

#[derive(Deserialize, Debug)]
pub struct WorkerConfig {
    #[serde(default = "default_max_files_per_task")]
    pub max_files_per_task: usize,
    #[serde(default)]
    pub max_parallel_workers: Option<usize>,
}

impl Default for WorkerConfig {
    fn default() -> Self {
        Self {
            max_files_per_task: default_max_files_per_task(),
            max_parallel_workers: None,
        }
    }
}

fn default_max_files_per_task() -> usize {
    5
}

impl Config {
    pub fn load(path: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let content = fs::read_to_string(path)?;
        let config = toml::from_str(&content)?;
        Ok(config)
    }
}
