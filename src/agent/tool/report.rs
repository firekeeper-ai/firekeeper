use serde::{Deserialize, Serialize};
use serde_json::json;
use tracing::debug;

use crate::agent::types::{Tool, ToolFunction};

/// A code review violation with file location and line range
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Violation {
    pub file: String,
    pub detail: String,
    pub start_line: u32,
    pub end_line: u32,
}

/// Create report tools for the agent
pub fn create_report_tools() -> Vec<Tool> {
    vec![Tool {
        tool_type: "function".to_string(),
        function: ToolFunction {
            name: "report".to_string(),
            description: "Report rule violations found during review".to_string(),
            parameters: json!({
                "type": "object",
                "properties": {
                    "violations": {
                        "type": "array",
                        "description": "List of violations",
                        "items": {
                            "type": "object",
                            "properties": {
                                "file": {"type": "string", "description": "File path"},
                                "detail": {"type": "string", "description": "Violation detail"},
                                "start_line": {"type": "integer", "description": "Start line (1-indexed)"},
                                "end_line": {"type": "integer", "description": "End line (inclusive)"}
                            },
                            "required": ["file", "detail", "start_line", "end_line"]
                        }
                    }
                },
                "required": ["violations"]
            }),
        },
    }]
}

/// Report violations and add them to the state
pub async fn report_violations(
    violations: Vec<Violation>,
    state: &mut Vec<Violation>,
) -> Result<String, String> {
    debug!("Reporting {} violations", violations.len());

    if violations.is_empty() {
        return Err("At least 1 violation is required".to_string());
    }

    state.extend(violations);
    Ok("OK".to_string())
}
