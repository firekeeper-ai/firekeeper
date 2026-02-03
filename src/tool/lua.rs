use mlua::Lua;
use std::sync::Arc;
use tiny_loop::tool::tool;
use tokio::sync::Mutex;

/// Tool for executing Lua scripts.
/// Use for math calculations, string manipulation, loops, and conditionals.
/// You can call multiple tools in one script, filter and concatenate results
/// to reduce loop steps and context usage.
#[tool]
pub async fn lua(
    /// Lua script to execute.
    ///
    /// Available tool functions (global): read(), fetch(), glob(), grep(), ls()
    /// Each function accepts a Lua table with the same parameters as the corresponding tool.
    ///
    /// Use print() to output results.
    ///
    /// Example:
    /// ```lua
    /// local files = ls({path = "src", depth = 0})
    /// for file in string.gmatch(files, "[^\n]+") do
    ///   local name = string.match(file, "f (.+)")
    ///   if name then
    ///     local diff = read({path = "src/" .. name})
    ///     if string.match(diff, "TODO") then
    ///       print("Found TODO in: " .. name)
    ///     end
    ///   end
    /// end
    /// ```
    script: String,
) -> String {
    let lua = Lua::new();
    let output = Arc::new(Mutex::new(String::new()));

    // Register tools
    if let Err(e) = register_tools(&lua) {
        return format!("Error setting up Lua environment: {}", e);
    }

    // Override print to capture output
    let output_clone = output.clone();
    let print_fn = lua
        .create_async_function(move |_, args: mlua::Variadic<mlua::Value>| {
            let output_clone = output_clone.clone();
            async move {
                let mut out = output_clone.lock().await;
                for (i, arg) in args.iter().enumerate() {
                    if i > 0 {
                        out.push('\t');
                    }
                    out.push_str(&format!("{:?}", arg));
                }
                out.push('\n');
                Ok(())
            }
        })
        .unwrap();
    lua.globals().set("print", print_fn).unwrap();

    match lua.load(&script).eval_async::<mlua::Value>().await {
        Ok(_) => output.lock().await.clone(),
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
