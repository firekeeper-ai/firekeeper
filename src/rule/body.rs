use serde::Deserialize;

#[derive(Deserialize, Debug)]
pub struct RuleBody {
    /// Human-readable rule name, not for LLM
    /// e.g. "Prefer Async instead of Promise Chain in JS/TS"
    pub name: String,
    /// Human-readable description, not for LLM, can be empty
    #[serde(default)]
    pub description: String,
    /// Instruction for LLM
    /// e.g. "for js/ts files, reject any Promise Chain, prefer async/await"
    pub instruction: String,
    /// Glob pattern strings
    /// e.g. ["src/**/*.ts"]
    #[serde(default = "default_scope")]
    pub scope: Vec<String>,
    /// Maximum files per task for this rule (overrides global config)
    #[serde(default)]
    pub max_files_per_task: Option<usize>,
}

pub fn default_scope() -> Vec<String> {
    vec!["**/*".to_string()]
}
