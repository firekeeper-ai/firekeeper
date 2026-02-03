use mlua::Lua;
use tiny_loop::tool::tool;

/// Tool for executing Lua scripts.
/// Use for math calculations, string manipulation, loops, and conditionals.
/// You can call multiple tools in one script, filter and concatenate results
/// to reduce loop steps and context usage.
#[tool]
pub async fn lua(
    /// Lua script to execute.
    ///
    /// Available functions: read, fetch, glob, grep, ls
    /// Each function accepts a Lua table with the same parameters as the corresponding tool.
    script: String,
) -> String {
    let lua = Lua::new();

    // Register tools
    if let Err(e) = register_tools(&lua) {
        return format!("Error setting up Lua environment: {}", e);
    }

    match lua.load(&script).eval_async::<mlua::Value>().await {
        Ok(value) => match value {
            mlua::Value::String(s) => s.to_string_lossy().to_string(),
            _ => serde_json::to_string(&value)
                .unwrap_or_else(|e| format!("Serialization error: {}. Value: {:?}", e, value)),
        },
        Err(e) => format!("Lua error: {}", e),
    }
}

fn register_tools(lua: &Lua) -> mlua::Result<()> {
    let globals = lua.globals();

    macro_rules! register_tool {
        ($name:ident) => {
            let tool_fn = lua.create_async_function(|_, args: mlua::Table| async move {
                let json_str =
                    serde_json::to_string(&args).map_err(|e| mlua::Error::external(e))?;
                let tool_args =
                    serde_json::from_str(&json_str).map_err(|e| mlua::Error::external(e))?;
                Ok(super::$name::$name(tool_args).await)
            })?;
            globals.set(stringify!($name), tool_fn)?;
        };
    }

    register_tool!(read);
    register_tool!(fetch);
    register_tool!(glob);
    register_tool!(grep);
    register_tool!(ls);

    Ok(())
}
