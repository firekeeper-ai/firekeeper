use serde::{Deserialize, Serialize};
use serde_json::json;

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
    vec![
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "think".to_string(),
                description: "Think through whether something is a violation. MUST be called before reporting any violations to reason about the findings.".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "reasoning": {
                            "type": "string",
                            "description": "Your reasoning about whether the code violates the rule, considering exceptions and context"
                        }
                    },
                    "required": ["reasoning"]
                }),
            },
        },
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "report".to_string(),
                description: "Report rule violations found during review. MUST call 'think' tool first.".to_string(),
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
        }
    ]
}

/// Think through whether something is a violation
pub async fn think(_reasoning: String) -> Result<String, String> {
    Ok("Continue with your analysis".to_string())
}

/// Report violations and add them to the state
pub async fn report_violations(
    violations: Vec<Violation>,
    state: &mut Vec<Violation>,
) -> Result<String, String> {
    if violations.is_empty() {
        return Err("At least 1 violation is required".to_string());
    }

    state.extend(violations);
    Ok("OK".to_string())
}
