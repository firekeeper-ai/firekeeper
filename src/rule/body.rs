use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use toml_scaffold::TomlScaffold;

#[derive(Deserialize, Serialize, Debug, Clone, JsonSchema, TomlScaffold)]
pub struct RuleBody {
    /// Human-readable rule name, invisible to LLM
    pub name: String,
    /// Human-readable description, invisible to LLM (optional)
    #[serde(default)]
    pub description: String,
    /// Detailed instructions for the LLM on how to check this rule
    pub instruction: String,
    /// Glob patterns to match files this rule applies to (optional, defaults to ["**/*"])
    #[serde(default = "default_scope")]
    pub scope: Vec<String>,
    /// Glob patterns to exclude files from this rule (optional, defaults to [])
    #[serde(default)]
    pub exclude: Vec<String>,
    /// Maximum number of files to review per task (optional, overrides global config).
    ///
    /// Increase for simple rules that only check changed files (e.g. scan for hardcoded credentials).
    ///
    /// Decrease for complex rules that scan many additional files (e.g. documentation sync).
    #[serde(default)]
    pub max_files_per_task: Option<usize>,
    /// Whether violations should block the pipeline (exit 1) (optional, defaults to true)
    #[serde(default = "default_blocking")]
    pub blocking: bool,
    /// Tip for downstream processors (e.g. coding agents) to fix violations (optional)
    #[serde(default)]
    pub tip: Option<String>,
}

pub fn default_scope() -> Vec<String> {
    vec!["**/*".to_string()]
}

fn default_blocking() -> bool {
    true
}

impl RuleBody {
    pub fn no_code_duplication() -> Self {
        Self {
            name: "No Code Duplication".into(),
            description: "Prevent duplicate code across files".into(),
            instruction: r#"
Ensure modified content does not duplicate code from other files.
- If duplicating an existing function, the code should call that function instead.
- If duplicating a code block, a shared function should be extracted.

Ignore acceptable duplication:
- Trivial code (simple one-liners, common patterns like error handling)
- Test code and test utilities
- Similar but contextually different logic (e.g., different validation rules)
- Common patterns like builder methods, getters/setters
- Standard boilerplate (e.g., CLI argument parsing, config loading)
- Factory methods or templates that intentionally duplicate configuration

Focus on substantial logic duplication:
- Business logic duplicated across multiple files (>30 lines)
- Complex algorithms or calculations repeated
- Data transformation logic that's identical
"#
            .into(),
            scope: default_scope(),
            exclude: vec![],
            // Low value for complex rule that scans many files
            max_files_per_task: Some(3),
            blocking: true,
            tip: Some(
                r#"
Extract common code into shared functions or modules.
"#
                .into(),
            ),
        }
    }

    pub fn no_magic_numbers() -> Self {
        Self {
            name: "No Magic Numbers".into(),
            description: "Prevent hardcoded numeric literals".into(),
            instruction: r#"
Reject unexplained numeric literals in production code.

Allowed numbers (not magic):
- 0, 1, -1 in common contexts (array indexing, loop increments, exit codes, boolean-like values)
- Numbers in test files
- Numbers in configuration files
- Numbers with nearby explanatory comments (within 3 lines)
- HTTP status codes (200, 404, etc.)
- Common time values with clear context (60 for seconds/minutes, 24 for hours, 1000 for milliseconds)
- Array/collection sizes in obvious contexts (e.g., Vec::with_capacity(10) in tests)

Reject as magic numbers:
- Business logic constants without explanation (e.g., threshold values, multipliers, limits)
- Arbitrary timeouts or delays without context
- Numeric configuration values hardcoded in logic
- Calculation constants without explanation
"#.into(),
            scope: default_scope(),
            exclude: vec![],
            // High value for simple rule that only checks changed files
            max_files_per_task: Some(10),
            blocking: true,
            tip: Some(r#"
Define constants with descriptive names or add explanatory comments.
"#.into()),
        }
    }

    pub fn no_hardcoded_credentials() -> Self {
        Self {
            name: "No Hardcoded Credentials".into(),
            description: "Prevent credential leaks".into(),
            instruction: r#"
Reject hardcoded credentials in code.

Forbidden:
- API keys, tokens, secrets (e.g., "sk-...", "Bearer ...", actual secret values)
- Passwords or password hashes
- Private keys or certificates
- OAuth client secrets
- Database connection strings with credentials

Allowed:
- Placeholder/example values (e.g., "your-api-key", "sk-xxxxxx", "<API_KEY>")
- Environment variable names (e.g., "API_KEY", "DATABASE_URL")
- Public URLs and endpoints
- Email addresses and contact information
- Test/mock credentials in test files clearly marked as fake
- Documentation examples with obvious placeholders
"#
            .into(),
            scope: default_scope(),
            exclude: vec![],
            // High value for simple rule that only checks changed files
            max_files_per_task: Some(10),
            blocking: true,
            tip: Some(
                r#"
Use environment variables or configuration files for credentials.
Replace real values with placeholders in examples.
"#
                .into(),
            ),
        }
    }
}
