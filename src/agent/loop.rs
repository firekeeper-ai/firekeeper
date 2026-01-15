use super::openai::{Message, Tool, ToolCall};

pub trait LLMProvider {
    async fn call(&mut self, messages: &[Message], tools: &[Tool]) -> Result<Message, Box<dyn std::error::Error>>;
}

pub trait ToolExecutor {
    fn execute(&self, tool_call: &ToolCall) -> String;
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
        self.messages.push(Message {
            role: role.to_string(),
            content: Some(content.to_string()),
            tool_calls: None,
            tool_call_id: None,
        });
    }

    pub async fn run(&mut self) -> Result<String, Box<dyn std::error::Error>> {
        loop {
            let message = self.provider.call(&self.messages, &self.tools).await?;
            self.messages.push(message.clone());

            if let Some(tool_calls) = &message.tool_calls {
                for tool_call in tool_calls {
                    let result = self.tool_executor.execute(tool_call);
                    self.messages.push(Message {
                        role: "tool".to_string(),
                        content: Some(result),
                        tool_calls: None,
                        tool_call_id: Some(tool_call.id.clone()),
                    });
                }
            } else if let Some(content) = &message.content {
                return Ok(content.clone());
            }
        }
    }
}
