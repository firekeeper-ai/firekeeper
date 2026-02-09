use mlua::Lua;
use tiny_loop::tool::tool;

use super::fetch::execute_fetch;
use super::sh::execute_sh;
use super::utils::{DEFAULT_NUM_CHARS, truncate_with_hint};

const TIMEOUT_SECS: u64 = 5;

/// Execute Lua scripts with access to sh() and fetch() functions.
/// Use for composing multiple tool calls, filtering results, and reducing context usage.
/// If you only need one sh() or fetch() call, use those tools directly instead.
///
/// Available functions:
/// - sh(command): Execute allowlisted shell commands (ls, cat, grep, find, head, tail, wc). Redirections not allowed.
/// - fetch(url): Fetch webpage and convert HTML to Markdown.
#[tool]
pub async fn lua(
    /// Lua script to execute.
    ///
    /// Example - Compose multiple commands and filter with Lua regex:
    /// ```lua
    /// -- Get file list and filter by pattern
    /// local files = sh("find . -name '*.rs'")
    /// local result = {}
    /// for line in files:gmatch("[^\n]+") do
    ///   if line:match("tool") then
    ///     table.insert(result, line)
    ///   end
    /// end
    /// return table.concat(result, "\n")
    /// ```
    ///
    /// Example - Fetch multiple URLs and combine:
    /// ```lua
    /// local page1 = fetch("https://example.com/page1")
    /// local page2 = fetch("https://example.com/page2")
    /// return page1 .. "\n---\n" .. page2
    /// ```
    script: String,
    /// Optional start character index (default: 0)
    start_char: Option<usize>,
    /// Optional number of characters to return (default: 5000)
    num_chars: Option<usize>,
) -> String {
    let lua = Lua::new();

    if let Err(e) = register_tools(&lua) {
        return format!("Failed to register tools: {}", e);
    }

    let result = match lua.load(&script).eval_async::<mlua::Value>().await {
        Ok(val) => match val {
            mlua::Value::String(s) => match s.to_str() {
                Ok(str_val) => str_val.to_string(),
                Err(e) => format!("Error converting Lua string to UTF-8: {}", e),
            },
            other => format!("{:?}", other),
        },
        Err(e) => format!("Lua error: {}", e),
    };

    truncate_with_hint(
        result,
        start_char.unwrap_or(0),
        num_chars.unwrap_or(DEFAULT_NUM_CHARS),
    )
}

fn register_tools(lua: &Lua) -> mlua::Result<()> {
    let sh_fn = lua.create_async_function(|_, command: String| async move {
        Ok(execute_sh(command, TIMEOUT_SECS).await)
    })?;
    lua.globals().set("sh", sh_fn)?;

    let fetch_fn =
        lua.create_async_function(|_, url: String| async move { Ok(execute_fetch(&url).await) })?;
    lua.globals().set("fetch", fetch_fn)?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_lua_basic() {
        let args = LuaArgs {
            script: "return 1 + 1".to_string(),
            start_char: None,
            num_chars: None,
        };
        let result = lua(args).await;
        assert!(result.contains("2"));
    }

    #[tokio::test]
    async fn test_lua_sh() {
        let args = LuaArgs {
            script: r#"return sh("cat /etc/hostname")"#.to_string(),
            start_char: None,
            num_chars: None,
        };
        let result = lua(args).await;
        // Should return some content (hostname)
        assert!(!result.is_empty());
        assert!(!result.contains("error"));
    }

    #[tokio::test]
    async fn test_lua_sh_not_allowed() {
        let args = LuaArgs {
            script: r#"return sh("rm -rf /")"#.to_string(),
            start_char: None,
            num_chars: None,
        };
        let result = lua(args).await;
        assert!(result.contains("not allowed"));
    }

    #[tokio::test]
    async fn test_lua_truncation() {
        let args = LuaArgs {
            script: format!(r#"return "{}""#, "a".repeat(100)),
            start_char: Some(0),
            num_chars: Some(50),
        };
        let result = lua(args).await;
        assert!(result.contains("Hint: Use start_char=50"));
    }
}
