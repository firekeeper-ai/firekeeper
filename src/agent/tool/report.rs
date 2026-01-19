use serde::{Deserialize, Serialize};
use serde_json::json;
use tracing::debug;

use crate::agent::types::{Tool, ToolFunction};

#[derive(Debug, Serialize, Deserialize)]
pub struct Violation {
    pub file: String,
    pub detail: String,
}

pub fn create_report_tools() -> Vec<Tool> {
    vec![
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "report_violations".to_string(),
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
                                    "detail": {"type": "string", "description": "Violation detail"}
                                },
                                "required": ["file", "detail"]
                            }
                        }
                    },
                    "required": ["violations"]
                }),
            },
        },
    ]
}

pub async fn report_violations(violations: Vec<Violation>) -> Result<String, String> {
    debug!("Reporting {} violations", violations.len());
    
    if violations.is_empty() {
        return Err("At least 1 violation is required".to_string());
    }
    
    Ok("OK".to_string())
}
