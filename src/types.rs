use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

/// A code review violation with file location and line range
#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema)]
pub struct Violation {
    /// File path
    pub file: String,
    /// Violation detail
    pub detail: String,
    /// Start line (1-indexed)
    pub start_line: u32,
    /// End line (inclusive)
    pub end_line: u32,
}
