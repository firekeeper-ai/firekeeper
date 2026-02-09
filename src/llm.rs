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
pub fn register_common_tools(agent: Agent) -> Agent {
    agent
        .tool(crate::tool::sh::sh)
        .tool(crate::tool::fetch::fetch)
        .tool(crate::tool::think::think)

    // Lua tool disabled: limited practical value currently.
    // Kept as reference for code execution pattern implementation.
    // .tool(crate::tool::lua::lua)
}
