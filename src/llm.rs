use tiny_loop::{Agent, llm::OpenAIProvider};

/// Create an LLM provider with the specified configuration
pub fn create_provider(
    api_key: &str,
    base_url: &str,
    model: &str,
    headers: &std::collections::HashMap<String, String>,
    body: &serde_json::Value,
) -> anyhow::Result<OpenAIProvider> {
    let mut provider = OpenAIProvider::new()
        .api_key(api_key)
        .base_url(base_url)
        .model(model);

    for (key, value) in headers {
        provider = provider.header(key, value)?;
    }

    if !body.is_null() {
        provider = provider.body(body.clone())?;
    }

    Ok(provider)
}

/// Register common tools (sh, fetch, think) to an agent
pub fn register_common_tools(agent: Agent, allowed_shell_commands: &[String]) -> Agent {
    let defs = vec![crate::tool::sh::sh_tool_def(allowed_shell_commands)];

    let allowed_cmds = allowed_shell_commands.to_vec();
    let exec = move |name: String, args: String| {
        let allowed_cmds = allowed_cmds.clone();
        async move {
            match name.as_str() {
                crate::tool::sh::ShArgs::TOOL_NAME => {
                    let args: crate::tool::sh::ShArgs = serde_json::from_str(&args).unwrap();
                    crate::tool::sh::execute_sh_args(args, &allowed_cmds).await
                }
                _ => format!("Unknown tool: {}", name),
            }
        }
    };

    agent
        .tool(crate::tool::fetch::fetch)
        .tool(crate::tool::think::think)
        .external(defs, exec)
}
