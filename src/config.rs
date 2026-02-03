use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::collections::HashMap;
use std::fs;
use toml_scaffold::TomlScaffold;

use crate::rule::body::RuleBody;

/// Configuration for Firekeeper
#[derive(Deserialize, Serialize, Debug, JsonSchema, TomlScaffold)]
#[serde(default)]
pub struct Config {
    /// LLM provider configuration
    pub llm: LlmConfig,
    /// Code review configuration
    pub review: ReviewConfig,
    /// Code review rules
    pub rules: Vec<crate::rule::body::RuleBody>,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            llm: LlmConfig::default(),
            review: ReviewConfig::default(),
            rules: vec![RuleBody {
                name: "Prefer Async instead of Promise Chain in JS/TS".into(),
                description: "".into(),
                instruction: "\nFor js/ts files:\nReject any Promise Chain, prefer async/await\n"
                    .into(),
                scope: vec!["src/**/*.ts".into()],
                max_files_per_task: None,
                blocking: true,
                tip: Some("tip".into()),
            }],
        }
    }
}

/// LLM provider configuration
#[derive(Deserialize, Serialize, Debug, JsonSchema, TomlScaffold)]
#[serde(default)]
pub struct LlmConfig {
    /// OpenAI compatible API base URL
    pub base_url: String,
    /// LLM model name
    pub model: String,
    /// Custom HTTP headers (optional)
    pub headers: HashMap<String, String>,
    /// Custom request body fields (optional)
    pub body: Value,
}

impl Default for LlmConfig {
    fn default() -> Self {
        Self {
            base_url: "https://openrouter.ai/api/v1".into(),
            model: "google/gemini-3-flash-preview".into(),
            headers: HashMap::from([
                (
                    "HTTP-Referer".to_string(),
                    "https://github.com/firekeeper-ai/firekeeper".to_string(),
                ),
                ("X-Title".to_string(), "firekeeper.ai".to_string()),
            ]),
            body: json!({
                "parallel_tool_calls": true
            }),
        }
    }
}

/// Code review configuration
#[derive(Deserialize, Serialize, Debug, JsonSchema, TomlScaffold)]
#[serde(default)]
pub struct ReviewConfig {
    /// Maximum number of files to review per task
    pub max_files_per_task: usize,
    /// Maximum number of parallel workers (optional, defaults to unlimited)
    pub max_parallel_workers: Option<usize>,
}

impl ReviewConfig {
    /// Default maximum number of files to review per task.
    /// 5 is a balanced value for most rules,
    /// allowing each worker to review multiple files without overwhelming the context.
    const DEFAULT_MAX_FILES_PER_TASK: usize = 5;
}

impl Default for ReviewConfig {
    fn default() -> Self {
        Self {
            max_files_per_task: Self::DEFAULT_MAX_FILES_PER_TASK,
            max_parallel_workers: None,
        }
    }
}

impl Config {
    pub fn load(path: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let content = fs::read_to_string(path)?;
        let config = toml::from_str(&content)?;
        Ok(config)
    }

    /// Apply config overrides using dot notation (e.g. "llm.model=gpt-4")
    ///
    /// Converts config to JSON, navigates to the field using dot-separated path,
    /// sets the value (auto-parsing JSON or treating as string), then converts back.
    pub fn apply_overrides(&mut self, overrides: &[String]) -> Result<(), String> {
        if overrides.is_empty() {
            return Ok(());
        }

        // Convert config to JSON for dynamic field access
        let mut json_value = serde_json::to_value(&self)
            .map_err(|e| format!("Failed to serialize config: {}", e))?;

        for override_str in overrides {
            // Parse "key=value" format
            let (key, value) = override_str
                .split_once('=')
                .ok_or_else(|| format!("Invalid override format: {}", override_str))?;

            // Split key into path parts (e.g. "llm.model" -> ["llm", "model"])
            let parts: Vec<&str> = key.split('.').collect();
            let mut current = &mut json_value;

            // Navigate through the JSON structure
            for (i, part) in parts.iter().enumerate() {
                if i == parts.len() - 1 {
                    // Last part: set the value
                    if let Some(obj) = current.as_object_mut() {
                        // Try parsing as JSON first (for numbers, bools, etc.), fallback to string
                        let parsed_value = serde_json::from_str(value)
                            .unwrap_or_else(|_| Value::String(value.to_string()));
                        obj.insert(part.to_string(), parsed_value);
                    } else {
                        return Err(format!("Cannot set field '{}' on non-object", part));
                    }
                } else {
                    // Intermediate part: navigate deeper
                    if let Some(obj) = current.as_object_mut() {
                        current = obj
                            .get_mut(*part)
                            .ok_or_else(|| format!("Unknown config key: {}", key))?;
                    } else {
                        return Err(format!("Cannot navigate through non-object at '{}'", part));
                    }
                }
            }
        }

        // Convert back to Config struct
        *self = serde_json::from_value(json_value)
            .map_err(|e| format!("Failed to deserialize config: {}", e))?;

        Ok(())
    }
}
