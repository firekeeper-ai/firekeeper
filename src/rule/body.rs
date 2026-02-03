use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use toml_scaffold::TomlScaffold;

#[derive(Deserialize, Serialize, Debug, Clone, JsonSchema, TomlScaffold)]
pub struct RuleBody {
    /// Human-readable rule name, not for LLM
    pub name: String,
    /// Human-readable description, not for LLM, optional
    #[serde(default)]
    pub description: String,
    /// Detailed instructions for the LLM on how to check this rule
    pub instruction: String,
    /// Glob patterns to match files this rule applies to (optional, defaults to ["**/*"])
    #[serde(default = "default_scope")]
    pub scope: Vec<String>,
    /// Maximum number of files to review per task (overrides global config)
    #[serde(default)]
    pub max_files_per_task: Option<usize>,
    /// Whether violations should block the pipeline (exit 1) (optional, defaults to true)
    #[serde(default = "default_blocking")]
    pub blocking: bool,
    /// Optional tip for downstream processors to fix violations
    #[serde(default)]
    pub tip: Option<String>,
}

pub fn default_scope() -> Vec<String> {
    vec!["**/*".to_string()]
}

fn default_blocking() -> bool {
    true
}
