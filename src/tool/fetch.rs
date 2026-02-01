use tiny_loop::tool::tool;

use super::utils::truncate_text_by_chars;

fn process_html(html: String, start_char: usize, num_chars: usize) -> String {
    let markdown = html2md::parse_html(&html);
    truncate_text_by_chars(markdown, start_char, num_chars)
}

/// Fetch a webpage and convert HTML to Markdown
#[tool]
pub async fn fetch(
    /// URL to fetch
    url: String,
    /// Optional start character index (default: 0)
    start_char: Option<usize>,
    /// Optional number of characters to return (default: 5000)
    num_chars: Option<usize>,
) -> String {
    let response = match reqwest::get(&url).await {
        Ok(r) => r,
        Err(e) => return format!("Error fetching URL: {}", e),
    };

    let html = match response.text().await {
        Ok(h) => h,
        Err(e) => return format!("Error reading response: {}", e),
    };

    process_html(html, start_char.unwrap_or(0), num_chars.unwrap_or(5000))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_process_html_basic() {
        let html = "<h1>Title</h1><p>Content</p>".to_string();
        let result = process_html(html, 0, 5000);
        assert!(result.contains("Title"));
        assert!(result.contains("Content"));
    }

    #[test]
    fn test_process_html_with_truncation() {
        let html = "<p>Hello World</p>".to_string();
        let result = process_html(html, 0, 5);
        assert!(result.contains("truncated"));
    }
}
