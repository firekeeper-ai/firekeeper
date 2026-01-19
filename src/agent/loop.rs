use super::types::{Message, Tool, ToolCall};
use tracing::{debug, trace, trace_span};

pub trait LLMProvider {
    async fn call(&mut self, messages: &[Message], tools: &[Tool]) -> Result<Message, Box<dyn std::error::Error>>;
}

pub trait ToolExecutor {
    async fn execute(&self, tool_call: &ToolCall) -> String;
}

pub struct AgentLoop<P: LLMProvider, T: ToolExecutor> {
    provider: P,
    tool_executor: T,
    messages: Vec<Message>,
    tools: Vec<Tool>,
}

impl<P: LLMProvider, T: ToolExecutor> AgentLoop<P, T> {
    pub fn new(provider: P, tool_executor: T, tools: Vec<Tool>) -> Self {
        Self {
            provider,
            tool_executor,
            messages: Vec::new(),
            tools,
        }
    }

    pub fn add_message(&mut self, role: &str, content: &str) {
        trace!("Adding message: role={}, content_len={}", role, content.len());
        self.messages.push(Message {
            role: role.to_string(),
            content: Some(content.to_string()),
            tool_calls: None,
            tool_call_id: None,
        });
    }

    pub async fn run(&mut self) -> Result<String, Box<dyn std::error::Error>> {
        trace!("Starting agent loop with {} messages", self.messages.len());
        let mut iteration = 0;
        
        loop {
            iteration += 1;
            trace_span!("Agent loop iteration {}", iteration);
            trace!("Calling LLM with {} messages and {} tools", self.messages.len(), self.tools.len());
            
            let message = self.provider.call(&self.messages, &self.tools).await?;
            
            // Log message content if present and non-empty
            if let Some(content) = &message.content {
                if !content.is_empty() {
                    debug!("LLM response content: {}", content);
                }
            }
            
            self.messages.push(message.clone());

            if let Some(tool_calls) = &message.tool_calls {
                debug!("LLM requested {} tool calls", tool_calls.len());
                for tool_call in tool_calls {
                    trace!("Executing tool: {}", tool_call.function.name);
                    let result = self.tool_executor.execute(tool_call).await;
                    debug!("Tool result: {}", result);
                    self.messages.push(Message {
                        role: "tool".to_string(),
                        content: Some(result),
                        tool_calls: None,
                        tool_call_id: Some(tool_call.id.clone()),
                    });
                }
            } else if let Some(content) = &message.content {
                debug!("Agent loop completed after {} iterations", iteration);
                return Ok(content.clone());
            }
        }
    }
}
