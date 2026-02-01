use tiny_loop::llm::OpenAIProvider;

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
