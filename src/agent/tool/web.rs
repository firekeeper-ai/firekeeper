use serde_json::json;
use tracing::debug;

use crate::agent::types::{Tool, ToolFunction};
use crate::agent::tool::fs::FsResult;

pub fn create_web_tools() -> Vec<Tool> {
    vec![
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "web_fetch".to_string(),
                description: "Fetch a webpage and convert HTML to Markdown".to_string(),
                parameters: json!({
                    "type": "object",
                    "properties": {
                        "url": {"type": "string", "description": "URL to fetch"}
                    },
                    "required": ["url"]
                }),
            },
        },
    ]
}

pub async fn fetch(url: &str) -> FsResult {
    debug!("Fetching URL: {}", url);
    
    let response = match reqwest::get(url).await {
        Ok(r) => r,
        Err(e) => return FsResult {
            success: false,
            content: format!("Error fetching URL: {}", e),
        },
    };
    
    let html = match response.text().await {
        Ok(h) => h,
        Err(e) => return FsResult {
            success: false,
            content: format!("Error reading response: {}", e),
        },
    };
    
    let markdown = html2md::parse_html(&html);
    
    FsResult {
        success: true,
        content: markdown,
    }
}
