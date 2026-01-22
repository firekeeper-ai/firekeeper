use serde::{Deserialize, Serialize};
use tracing::trace;

use crate::agent::types::{Message, Tool};

#[derive(Serialize)]
struct ChatRequest {
    model: String,
    messages: Vec<Message>,
    tools: Vec<Tool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    temperature: Option<f32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    max_tokens: Option<u32>,
}

#[derive(Deserialize)]
struct ChatResponse {
    choices: Vec<Choice>,
}

#[derive(Deserialize)]
struct Choice {
    message: Message,
}

/// OpenAI-compatible LLM provider
pub struct OpenAIProvider {
    client: reqwest::Client,
    base_url: String,
    api_key: String,
    model: String,
    temperature: Option<f32>,
    max_tokens: u32,
}

impl OpenAIProvider {
    /// Create a new OpenAI provider
    pub fn new(base_url: String, api_key: String, model: String, temperature: Option<f32>, max_tokens: u32) -> Self {
        Self {
            client: reqwest::Client::new(),
            base_url,
            api_key,
            model,
            temperature,
            max_tokens,
        }
    }
}

impl crate::agent::r#loop::LLMProvider for OpenAIProvider {
    async fn call(
        &mut self,
        messages: &[Message],
        tools: &[Tool],
    ) -> Result<Message, Box<dyn std::error::Error>> {
        trace!(
            "Request: {} messages, {} tools",
            messages.len(),
            tools.len()
        );

        let request = ChatRequest {
            model: self.model.clone(),
            messages: messages.to_vec(),
            tools: tools.to_vec(),
            temperature: self.temperature,
            max_tokens: Some(self.max_tokens),
        };

        let response = self
            .client
            .post(format!("{}/chat/completions", self.base_url))
            .header("Authorization", format!("Bearer {}", self.api_key))
            .header("Content-Type", "application/json")
            .json(&request)
            .send()
            .await?;

        let chat_response: ChatResponse = response.json().await?;
        trace!("Response has {} choices", chat_response.choices.len());

        Ok(chat_response.choices[0].message.clone()) // First choice is the primary response
    }
}
