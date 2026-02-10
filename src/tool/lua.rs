use mlua::{Lua, LuaSerdeExt};
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use tiny_loop::types::{Parameters, ToolDefinition, ToolFunction};

use super::fetch::execute_fetch;
use super::sh::execute_sh_raw;
use super::utils::{DEFAULT_NUM_CHARS, truncate_with_hint};

const TIMEOUT_SECS: u64 = 5;

#[derive(Debug, Serialize, Deserialize, JsonSchema)]
pub struct LuaArgs {
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
    pub script: String,
    /// Optional start character index (default: 0)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub start_char: Option<usize>,
    /// Optional number of characters to return (default: 5000)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub num_chars: Option<usize>,
}

impl LuaArgs {
    pub const TOOL_NAME: &'static str = "lua";
}

pub fn lua_tool_def(allowed_commands: &[String]) -> ToolDefinition {
    let commands_str = allowed_commands.join(", ");
    ToolDefinition {
        tool_type: "function".into(),
        function: ToolFunction {
            name: LuaArgs::TOOL_NAME.into(),
            description: format!(
                "Execute Lua scripts with access to sh() and fetch() functions.\n\
                Use for composing multiple tool calls, filtering results, and reducing context usage.\n\
                If you only need one sh() or fetch() call, use those tools directly instead.\n\n\
                Available functions:\n\
                - sh(command): Execute allowlisted shell commands ({}). Redirections not allowed.\n\
                - fetch(url): Fetch webpage and convert HTML to Markdown.",
                commands_str
            ),
            parameters: Parameters::from_type::<LuaArgs>(),
        },
    }
}

pub async fn execute_lua_args(args: LuaArgs, allowed_commands: &[String]) -> String {
    let lua = Lua::new();

    if let Err(e) = register_tools(&lua, allowed_commands) {
        return format!("Failed to register tools: {}", e);
    }

    let result = match lua.load(&args.script).eval_async::<mlua::Value>().await {
        Ok(val) => match val {
            mlua::Value::String(s) => match s.to_str() {
                Ok(str_val) => str_val.to_string(),
                Err(e) => format!("Error converting Lua string to UTF-8: {}", e),
            },
            other => match lua.from_value::<serde_json::Value>(other.clone()) {
                Ok(json_val) => {
                    serde_json::to_string(&json_val).unwrap_or_else(|e| format!("{:?}", e))
                }
                Err(e) => format!("{:?}", e),
            },
        },
        Err(e) => format!("Lua error: {}", e),
    };

    truncate_with_hint(
        result,
        args.start_char.unwrap_or(0),
        args.num_chars.unwrap_or(DEFAULT_NUM_CHARS),
    )
}

fn register_tools(lua: &Lua, allowed_commands: &[String]) -> mlua::Result<()> {
    let allowed_commands = allowed_commands.to_vec();
    let sh_fn = lua.create_async_function(move |_, command: String| {
        let allowed_commands = allowed_commands.clone();
        async move { Ok(execute_sh_raw(command, TIMEOUT_SECS, &allowed_commands).await) }
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
        let allowed = vec!["ls".to_string()];
        let result = execute_lua_args(args, &allowed).await;
        assert!(result.contains("2"));
    }

    #[tokio::test]
    async fn test_lua_sh() {
        let args = LuaArgs {
            script: r#"return sh("cat /etc/hostname")"#.to_string(),
            start_char: None,
            num_chars: None,
        };
        let allowed = vec!["cat".to_string()];
        let result = execute_lua_args(args, &allowed).await;
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
        let allowed = vec!["ls".to_string()];
        let result = execute_lua_args(args, &allowed).await;
        assert!(result.contains("not allowed"));
    }

    #[tokio::test]
    async fn test_lua_truncation() {
        let args = LuaArgs {
            script: format!(r#"return "{}""#, "a".repeat(100)),
            start_char: Some(0),
            num_chars: Some(50),
        };
        let allowed = vec!["ls".to_string()];
        let result = execute_lua_args(args, &allowed).await;
        assert!(result.contains("Hint: Use start_char=50"));
    }

    #[tokio::test]
    async fn test_lua_json_serialization() {
        let args = LuaArgs {
            script: r#"return {a = 1, b = "test", c = true}"#.to_string(),
            start_char: None,
            num_chars: None,
        };
        let allowed = vec!["ls".to_string()];
        let result = execute_lua_args(args, &allowed).await;
        assert!(result.contains(r#""a":1"#) || result.contains(r#""a": 1"#));
        assert!(result.contains(r#""b":"test""#) || result.contains(r#""b": "test""#));
        assert!(result.contains(r#""c":true"#) || result.contains(r#""c": true"#));
    }
}
