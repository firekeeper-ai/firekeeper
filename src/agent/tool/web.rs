use serde_json::json;
use tracing::debug;

use crate::agent::types::{Tool, ToolFunction};

pub fn create_web_tools() -> Vec<Tool> {
    vec![
        Tool {
            tool_type: "function".to_string(),
            function: ToolFunction {
                name: "fetch".to_string(),
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

pub async fn fetch(url: &str) -> Result<String, String> {
    debug!("Fetching URL: {}", url);
    
    let response = match reqwest::get(url).await {
        Ok(r) => r,
        Err(e) => return Err(format!("Error fetching URL: {}", e)),
    };
    
    let html = match response.text().await {
        Ok(h) => h,
        Err(e) => return Err(format!("Error reading response: {}", e)),
    };
    
    let markdown = html2md::parse_html(&html);
    
    Ok(markdown)
}
