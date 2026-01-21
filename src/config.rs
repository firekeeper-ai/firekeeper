use serde::Deserialize;
use std::fs;

pub const DEFAULT_BASE_URL: &str = "https://openrouter.ai/api/v1";
pub const DEFAULT_MODEL: &str = "google/gemini-3-flash-preview";
pub const DEFAULT_MAX_FILES_PER_TASK: usize = 5;

/// Configuration for Firekeeper code review
#[derive(Deserialize, Debug)]
pub struct Config {
    /// LLM provider configuration
    pub llm: LlmConfig,
    /// Worker configuration
    #[serde(default)]
    pub worker: WorkerConfig,
    /// Review rules
    pub rules: Vec<crate::rule::body::RuleBody>,
}

/// LLM provider configuration
#[derive(Deserialize, Debug)]
pub struct LlmConfig {
    /// LLM API base URL
    #[serde(default = "default_base_url")]
    pub base_url: String,
    /// LLM model name
    #[serde(default = "default_model")]
    pub model: String,
}

fn default_base_url() -> String {
    DEFAULT_BASE_URL.to_string()
}

fn default_model() -> String {
    DEFAULT_MODEL.to_string()
}

/// Worker configuration
#[derive(Deserialize, Debug)]
pub struct WorkerConfig {
    /// Maximum number of files per review task
    #[serde(default = "default_max_files_per_task")]
    pub max_files_per_task: usize,
    /// Maximum number of parallel workers
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
    DEFAULT_MAX_FILES_PER_TASK
}

impl Config {
    pub fn load(path: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let content = fs::read_to_string(path)?;
        let config = toml::from_str(&content)?;
        Ok(config)
    }
}
