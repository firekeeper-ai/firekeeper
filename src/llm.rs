use tiny_loop::{Agent, llm::OpenAIProvider};

/// Create an LLM provider with the specified configuration
pub fn create_provider(
    api_key: &str,
    base_url: &str,
    model: &str,
    temperature: Option<f32>,
    max_tokens: u32,
) -> OpenAIProvider {
    OpenAIProvider::new()
        .api_key(api_key)
        .base_url(base_url)
        .model(model)
        .temperature(temperature)
        .max_tokens(max_tokens)
}

/// Register common tools (read, fetch, ls, grep, glob, think) to an agent
pub fn register_common_tools(agent: Agent) -> Agent {
    agent
        .tool(crate::tool::read::read)
        .tool(crate::tool::fetch::fetch)
        .tool(crate::tool::ls::ls)
        .tool(crate::tool::grep::grep)
        .tool(crate::tool::glob::glob)
        .tool(crate::tool::think::think)
}
