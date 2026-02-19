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
    /// Glob patterns to exclude from the matched scope (optional, defaults to [])
    #[serde(default)]
    pub exclude: Vec<String>,
    /// Maximum number of files to review per task (optional, overrides global config).
    /// Increase for simple rules that only check changed files (e.g. scan for hardcoded credentials).
    /// Decrease for complex rules that scan many additional files (e.g. documentation sync).
    #[serde(default)]
    pub max_files_per_task: Option<usize>,
    /// Rule-specific resources to include in review context.
    #[serde(default)]
    pub resources: Vec<String>,
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

fn default_non_code_exclude() -> Vec<String> {
    vec![
        "**/*.md".into(),
        "**/*.toml".into(),
        "**/*.json".into(),
        "**/*.yaml".into(),
        "**/*.yml".into(),
        "**/*.xml".into(),
        "**/*.ini".into(),
        "**/*.cfg".into(),
        "**/*.conf".into(),
        "**/*.lock".into(),
        "**/*ignore".into(),
    ]
}

fn default_lock_and_ignore_exclude() -> Vec<String> {
    vec!["*.lock".into(), "*lock.json".into(), "*ignore".into()]
}

impl RuleBody {
    pub fn config_file_comments() -> Self {
        Self {
            name: "Firekeeper Config Comments".into(),
            description: "Ensure firekeeper.toml has correct documentation comments".into(),
            instruction: r#"Check if firekeeper.toml has missing documentation comments.

Steps:
1. Check if all fields have documentation comments
2. Report violations if any field is missing a comment

Violation criteria - Report if:
- Any field lacks a documentation comment
"#
            .into(),
            scope: vec!["firekeeper.toml".into()],
            exclude: vec![],
            // Only 1 file needs to be reviewed
            max_files_per_task: Some(1),
            blocking: true,
            tip: Some(r#"Use `firekeeper config format [--config firekeeper.toml]` to re-render the config file
"#.into()),
            resources: vec!["file://firekeeper.toml".into()],
        }
    }

    pub fn no_magic_numbers() -> Self {
        Self {
            name: "No Magic Numbers".into(),
            description: "Prevent hardcoded numeric literals".into(),
            instruction: r#"Check for unexplained numeric literals in the provided diff.

Steps:
1. Identify numeric literals in the diff
2. Check if numbers have explanatory comments or clear context
3. Report violations for unexplained numbers

Violation criteria - Report if:
- Business logic constants without explanation (thresholds, multipliers, limits)
- Arbitrary timeouts or delays without context
- Numeric configuration values hardcoded in logic without comments
- Calculation constants without explanation

Exemptions - Do NOT report:
- 0, 1, -1 in common contexts (indexing, loops, exit codes, boolean-like)
- Numbers in test files
- Numbers in configuration files
- Numbers with nearby comments (within 3 lines above or inline)
- Numbers in default value functions with descriptive context
- HTTP status codes (200, 404, etc.)
- Common time values with clear context (60 for seconds, 24 for hours, 1000 for ms)
- Array/collection sizes in obvious contexts
"#
            .into(),
            scope: default_scope(),
            exclude: default_non_code_exclude(),
            // High value for simple rule that only checks changed files
            max_files_per_task: Some(10),
            blocking: true,
            tip: Some(
                r#"Define constants with descriptive names or add explanatory comments.
"#
                .into(),
            ),
            resources: vec![],
        }
    }

    pub fn no_hardcoded_credentials() -> Self {
        Self {
            name: "No Hardcoded Credentials".into(),
            description: "Prevent credential leaks".into(),
            instruction: r#"Check for hardcoded credentials in the provided diff.

Steps:
1. Scan the diff for credential-like strings
2. Determine if they are real credentials or placeholders
3. Report violations for any real credentials found

Violation criteria - Report if ANY found:
- API keys, tokens, secrets (e.g., "sk-...", "Bearer ...", actual secret values)
- Passwords or password hashes
- Private keys or certificates
- OAuth client secrets
- Database connection strings with credentials

Exemptions - Do NOT report:
- Placeholder/example values (e.g., "your-api-key", "sk-xxxxxx", "<API_KEY>")
- Environment variable names (e.g., "API_KEY", "DATABASE_URL")
- Public URLs and endpoints
- Email addresses and contact information
- Test/mock credentials in test files clearly marked as fake
- Documentation examples with obvious placeholders
"#
            .into(),
            scope: default_scope(),
            exclude: default_lock_and_ignore_exclude(),
            // High value for simple rule that only checks changed files
            max_files_per_task: Some(10),
            blocking: true,
            tip: Some(
                r#"Use environment variables or configuration files for credentials.
Replace real values with placeholders in examples.
"#
                .into(),
            ),
            resources: vec![],
        }
    }

    pub fn no_code_duplication() -> Self {
        Self {
            name: "No Code Duplication".into(),
            description: "Prevent duplicate code across files".into(),
            instruction: r#"Check if modified code duplicates existing code in other files.

Steps:
1. Read focused files to understand the modified logic
2. Search for similar code patterns in other files
3. Report violations if substantial duplication is found

Violation criteria - Report if:
- Business logic duplicated across files (>30 lines of similar code)
- Complex algorithms or calculations repeated identically
- Data transformation logic that's the same in multiple places
- Functions that could be extracted into shared utilities

Exemptions - Do NOT report:
- Trivial code (one-liners, common patterns like error handling)
- Test code and test utilities
- Similar but contextually different logic (e.g., different validation rules)
- Common patterns (builder methods, getters/setters)
- Standard boilerplate (CLI parsing, config loading)
- Factory methods or templates with intentional configuration duplication
"#
            .into(),
            scope: default_scope(),
            exclude: default_non_code_exclude(),
            // Low value for complex rule that scans many files
            max_files_per_task: Some(3),
            blocking: true,
            tip: Some(
                r#"Extract common code into shared functions or modules.
"#
                .into(),
            ),
            resources: vec!["sh://git ls-files".into()],
        }
    }
}
