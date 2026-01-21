use serde::{Deserialize, Serialize};
use tracing::trace;

use crate::agent::types::{Message, Tool};

#[derive(Serialize)]
struct ChatRequest {
    model: String,
    messages: Vec<Message>,
    tools: Vec<Tool>,
    temperature: f32,
}

#[derive(Deserialize)]
struct ChatResponse {
    choices: Vec<Choice>,
}

#[derive(Deserialize)]
struct Choice {
    message: Message,
}

pub struct OpenAIProvider {
    client: reqwest::Client,
    base_url: String,
    api_key: String,
    model: String,
}

impl OpenAIProvider {
    pub fn new(base_url: String, api_key: String, model: String) -> Self {
        Self {
            client: reqwest::Client::new(),
            base_url,
            api_key,
            model,
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
            temperature: 0.0,
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

        Ok(chat_response.choices[0].message.clone())
    }
}
