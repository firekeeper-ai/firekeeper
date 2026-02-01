use tiny_loop::tool::tool;

use super::utils::truncate_text;

/// Read file contents with optional character range
#[tool]
pub async fn read(
    /// File path
    path: String,
    /// Optional start character index (default: 0)
    start: Option<usize>,
    /// Optional length in characters (default: 5000)
    len: Option<usize>,
) -> String {
    match tokio::fs::read_to_string(&path).await {
        Ok(content) => truncate_text(content, start.unwrap_or(0), len.unwrap_or(5000)),
        Err(e) => format!("Error reading file: {}", e),
    }
}
