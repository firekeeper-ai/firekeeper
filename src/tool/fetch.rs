use tiny_loop::tool::tool;

use super::utils::{DEFAULT_NUM_CHARS, truncate_with_hint};

/// Fetch webpages and convert HTML to Markdown
#[tool]
pub async fn fetch(
    /// URLs to fetch
    url: Vec<String>,
    /// Optional start character index (default: 0)
    start_char: Option<usize>,
    /// Optional number of characters to return (default: 5000)
    num_chars: Option<usize>,
) -> String {
    if url.len() == 1 {
        return fetch_one(&url[0], start_char, num_chars).await;
    }

    let mut results = Vec::with_capacity(url.len());
    for u in url {
        let content = fetch_one(&u, start_char, num_chars).await;
        results.push(format!("=== {} ===\n{}", u, content));
    }
    results.join("\n\n")
}

async fn fetch_one(url: &str, start_char: Option<usize>, num_chars: Option<usize>) -> String {
    let markdown = execute_fetch(url).await;
    truncate_with_hint(
        markdown,
        start_char.unwrap_or(0),
        num_chars.unwrap_or(DEFAULT_NUM_CHARS),
    )
}

pub async fn execute_fetch(url: &str) -> String {
    let response = match reqwest::get(url).await {
        Ok(r) => r,
        Err(e) => return format!("Error fetching URL: {}", e),
    };

    let html = match response.text().await {
        Ok(h) => h,
        Err(e) => return format!("Error reading response: {}", e),
    };

    html2md::parse_html(&html)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_fetch_html_basic() {
        let html = "<h1>Title</h1><p>Content</p>".to_string();
        let markdown = html2md::parse_html(&html);
        let result = truncate_with_hint(markdown, 0, 5000);
        assert!(result.contains("Title"));
        assert!(result.contains("Content"));
    }

    #[test]
    fn test_fetch_html_with_truncation() {
        let html = "<p>Hello World</p>".to_string();
        let markdown = html2md::parse_html(&html);
        let result = truncate_with_hint(markdown, 0, 5);
        assert!(result.contains("truncated"));
    }
}
