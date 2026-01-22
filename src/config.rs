use serde::Deserialize;
use std::fs;

pub const DEFAULT_BASE_URL: &str = "https://openrouter.ai/api/v1";
pub const DEFAULT_MODEL: &str = "google/gemini-3-flash-preview";
pub const DEFAULT_MAX_FILES_PER_TASK: usize = 5;

pub fn default_config_template() -> String {
    let default_scope = crate::rule::body::default_scope();
    format!(
        r#"[llm]
base_url = "{}"
model = "{}"
# Temperature for LLM sampling (optional, omit for model default)
# temperature = 0.1
# Maximum tokens for LLM response (optional, defaults to 4096)
# max_tokens = 4096

[worker]
# Maximum number of files to process per task (optional, defaults to {})
max_files_per_task = {}
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
# Glob patterns to match files this rule applies to (optional, defaults to {:?})
scope = {:?}
# Maximum number of files to process per task (overrides global config)
# max_files_per_task = {}
# Whether violations should block the pipeline (optional, defaults to true)
# blocking = true
"#,
        DEFAULT_BASE_URL,
        DEFAULT_MODEL,
        DEFAULT_MAX_FILES_PER_TASK,
        DEFAULT_MAX_FILES_PER_TASK,
        default_scope,
        default_scope,
        DEFAULT_MAX_FILES_PER_TASK
    )
}

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
    /// Temperature for LLM sampling (optional)
    #[serde(default)]
    pub temperature: Option<f32>,
    /// Maximum tokens for LLM response
    #[serde(default = "default_max_tokens")]
    pub max_tokens: u32,
}

fn default_base_url() -> String {
    DEFAULT_BASE_URL.to_string()
}

fn default_model() -> String {
    DEFAULT_MODEL.to_string()
}

fn default_max_tokens() -> u32 {
    4096
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
