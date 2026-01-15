use crate::agent::openai::OpenAIProvider;
use crate::agent::r#loop::AgentLoop;
use crate::rule::body::RuleBody;

pub async fn worker(
    rule: &RuleBody,
    files: Vec<String>,
    base_url: &str,
    api_key: &str,
    model: &str,
) -> Result<(), Box<dyn std::error::Error>> {
    println!("Worker: reviewing {} files for rule '{}'", files.len(), rule.name);
    
    let provider = OpenAIProvider::new(base_url.to_string(), api_key.to_string(), model.to_string());
    let mut agent = AgentLoop::new(provider, NoOpToolExecutor, vec![]);
    
    agent.add_message("user", "Hello, can you help me review some code?");
    
    let response = agent.run().await?;
    println!("Agent response for rule '{}': {}", rule.name, response);
    
    Ok(())
}

struct NoOpToolExecutor;

impl crate::agent::r#loop::ToolExecutor for NoOpToolExecutor {
    fn execute(&self, _tool_call: &crate::agent::openai::ToolCall) -> String {
        String::new()
    }
}
