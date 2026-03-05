use crate::review::worker::SHUTDOWN_POLL_INTERVAL_MS;
use crate::review::worker::{WorkerResult, build_user_message, load_resources, log_completion};
use crate::rule::body::RuleBody;
use std::collections::HashMap;
use std::sync::Arc;
use tiny_loop::types::{Message, TimedMessage, ToolDefinition, UserMessage};
use tokio::sync::Mutex;
use tracing::{debug, error, warn};

pub async fn worker_acp(
    worker_id: String,
    rule: &RuleBody,
    files: Vec<String>,
    all_changed_files: Vec<String>,
    commit_messages: String,
    command: &str,
    args: &[String],
    mode: &str,
    env: &HashMap<String, String>,
    context_server_url: &str,
    diffs: HashMap<String, String>,
    _trace_enabled: bool,
    shutdown: Arc<Mutex<bool>>,
    is_root_base: bool,
    global_resources: Vec<String>,
    timeout_secs: u64,
    start: std::time::Instant,
) -> Result<WorkerResult, Box<dyn std::error::Error>> {
    use agent_client_protocol::*;
    use tokio_util::compat::{TokioAsyncReadCompatExt, TokioAsyncWriteCompatExt};

    debug!(
        "[Worker {}] Spawning ACP agent: {} {:?}",
        worker_id, command, args
    );

    // Prepare environment variables
    let mut child_env = env.clone();
    child_env.insert(
        "FIREKEEPER_CONTEXT_SERVER".to_string(),
        context_server_url.to_string(),
    );
    child_env.insert("FIREKEEPER_WORKER_ID".to_string(), worker_id.clone());

    // Spawn ACP agent process
    let mut child = tokio::process::Command::new(command)
        .args(args)
        .envs(child_env)
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::inherit())
        .spawn()?;

    let stdin = child.stdin.take().unwrap().compat_write();
    let stdout = child.stdout.take().unwrap().compat();

    // Load resources
    let mut all_resources = global_resources.clone();
    all_resources.extend(rule.resources.clone());
    all_resources.sort();
    all_resources.dedup();
    let resources_content = load_resources(&all_resources).await;

    // Build user message as ContentBlock with system message prepended
    let user_message_text = format!(
        "{}\n\n{}",
        super::SYSTEM_MESSAGE,
        build_user_message(
            &files,
            &all_changed_files,
            &commit_messages,
            is_root_base,
            &rule.instruction,
            &diffs,
            &resources_content,
        )
    );
    let content_blocks = vec![ContentBlock::Text(TextContent::new(user_message_text))];

    // Run ACP communication in LocalSet for !Send futures
    let local = tokio::task::LocalSet::new();
    let worker_id_clone = worker_id.clone();
    let client = FirekeeperClient::new();
    let messages_ref = client.messages.clone();
    let tools_ref = client.tools.clone();

    let result = local
        .run_until(async move {
            // Create ACP client connection
            let (connection, io_task) = ClientSideConnection::new(client, stdin, stdout, |fut| {
                tokio::task::spawn_local(fut);
            });

            let worker_id_for_io = worker_id_clone.clone();
            tokio::task::spawn_local(async move {
                if let Err(e) = io_task.await {
                    error!("[Worker {}] ACP IO task error: {}", worker_id_for_io, e);
                }
            });

            // Initialize agent
            let init_req = InitializeRequest::new(ProtocolVersion::LATEST)
                .client_info(Implementation::new("firekeeper", env!("CARGO_PKG_VERSION")));

            connection.initialize(init_req).await?;

            // Create session with workspace path
            let workspace_path = std::env::current_dir()?;
            let session_req = NewSessionRequest::new(workspace_path);
            let session_resp = connection.new_session(session_req).await?;
            let session_id = session_resp.session_id;

            let worker_id_for_mode = worker_id_clone.clone();

            // Set mode using modern session config option (preferred) or deprecated set_mode
            if !mode.is_empty() {
                let set_config_req = SetSessionConfigOptionRequest::new(
                    session_id.clone(),
                    agent_client_protocol::SessionConfigId::new("mode"),
                    agent_client_protocol::SessionConfigValueId::new(mode),
                );
                let config_err = connection.set_session_config_option(set_config_req).await;
                if config_err.is_err() {
                    let set_mode_req = SetSessionModeRequest::new(
                        session_id.clone(),
                        agent_client_protocol::SessionModeId::new(mode),
                    );
                    if connection.set_session_mode(set_mode_req).await.is_err() {
                        error!(
                            "[Worker {}] Failed to set mode '{}' via both session/set_config_option and session/set_mode",
                            worker_id_for_mode, mode
                        );
                    }
                }
            }

            // Send prompt
            let prompt_req = PromptRequest::new(session_id.clone(), content_blocks);
            connection.prompt(prompt_req).await?;

            Ok::<(), Box<dyn std::error::Error>>(())
        })
        .await;

    // Wait with timeout and cancellation
    let cancelled = tokio::select! {
        _ = async { result } => {
            let _ = child.kill().await;
            false
        }
        _ = tokio::time::sleep(tokio::time::Duration::from_secs(timeout_secs)) => {
            warn!("[Worker {}] ACP agent timed out after {}s", worker_id, timeout_secs);
            let _ = child.kill().await;
            true
        }
        _ = async {
            loop {
                tokio::time::sleep(tokio::time::Duration::from_millis(SHUTDOWN_POLL_INTERVAL_MS)).await;
                if *shutdown.lock().await {
                    break;
                }
            }
        } => {
            warn!("[Worker {}] ACP agent cancelled by shutdown", worker_id);
            let _ = child.kill().await;
            true
        }
    };

    let elapsed = start.elapsed().as_secs_f64();
    log_completion(cancelled, &worker_id, &rule.name, elapsed);

    // Collect trace data if enabled
    let (messages, tools) = if _trace_enabled {
        let messages = messages_ref.lock().await.clone();
        let tools = tools_ref.lock().await.clone();
        (Some(messages), Some(tools))
    } else {
        (None, None)
    };

    // Violations will be collected by report server
    Ok(WorkerResult {
        worker_id,
        rule: rule.clone(),
        files,
        blocking: rule.blocking,
        violations: vec![],
        messages,
        tools,
        elapsed_secs: elapsed,
    })
}

// ACP client implementation that collects trace data
struct FirekeeperClient {
    messages: Arc<Mutex<Vec<TimedMessage>>>,
    tools: Arc<Mutex<Vec<ToolDefinition>>>,
    start_time: Arc<Mutex<std::time::Instant>>,
}

impl FirekeeperClient {
    fn new() -> Self {
        Self {
            messages: Arc::new(Mutex::new(Vec::new())),
            tools: Arc::new(Mutex::new(Vec::new())),
            start_time: Arc::new(Mutex::new(std::time::Instant::now())),
        }
    }
}

#[async_trait::async_trait(?Send)]
impl agent_client_protocol::Client for FirekeeperClient {
    async fn session_notification(
        &self,
        notification: agent_client_protocol::SessionNotification,
    ) -> agent_client_protocol::Result<()> {
        use agent_client_protocol::{ContentBlock, SessionUpdate};

        let mut messages = self.messages.lock().await;

        match notification.update {
            // User message chunk: buffer/accumulate if last message is also user
            SessionUpdate::UserMessageChunk(chunk) => {
                if let ContentBlock::Text(text) = chunk.content {
                    let chunk_elapsed = self.start_time.lock().await.elapsed();
                    if let Some(last) = messages.last_mut() {
                        if let Message::User(ref mut msg) = last.message {
                            msg.content.push_str(&text.text);
                            last.timestamp = std::time::SystemTime::now();
                            last.elapsed += chunk_elapsed;
                            *self.start_time.lock().await = std::time::Instant::now();
                            return Ok(());
                        }
                    }
                    *self.start_time.lock().await = std::time::Instant::now();
                    messages.push(TimedMessage {
                        message: Message::User(UserMessage { content: text.text }),
                        timestamp: std::time::SystemTime::now(),
                        elapsed: chunk_elapsed,
                    });
                }
            }
            // Agent message chunk: buffer/accumulate if last message is also agent
            SessionUpdate::AgentMessageChunk(chunk) => {
                if let ContentBlock::Text(text) = chunk.content {
                    let chunk_elapsed = self.start_time.lock().await.elapsed();
                    if let Some(last) = messages.last_mut() {
                        if let Message::Assistant(ref mut msg) = last.message {
                            msg.content.push_str(&text.text);
                            last.timestamp = std::time::SystemTime::now();
                            last.elapsed += chunk_elapsed;
                            *self.start_time.lock().await = std::time::Instant::now();
                            return Ok(());
                        }
                    }
                    *self.start_time.lock().await = std::time::Instant::now();
                    messages.push(TimedMessage {
                        message: Message::Assistant(tiny_loop::types::AssistantMessage {
                            content: text.text,
                            tool_calls: None,
                        }),
                        timestamp: std::time::SystemTime::now(),
                        elapsed: chunk_elapsed,
                    });
                }
            }
            // Tool call: find existing by id and update, or add new tool call to assistant message
            SessionUpdate::ToolCall(tool_call) => {
                let tool_call_id = tool_call.tool_call_id.to_string();
                let title = tool_call.title.clone();
                let chunk_elapsed = self.start_time.lock().await.elapsed();
                let mut found = false;

                // Search backwards for existing tool call with matching id
                for msg in messages.iter_mut().rev() {
                    if let Message::Assistant(ref mut am) = msg.message {
                        if let Some(ref mut calls) = am.tool_calls {
                            if let Some(call) = calls.iter_mut().find(|c| c.id == tool_call_id) {
                                // Update title if not empty
                                if !title.is_empty() {
                                    call.function.name = title.clone();
                                }
                                // Update arguments from raw_input if available and not empty
                                if let Some(raw_input) = &tool_call.raw_input {
                                    let args = serde_json::to_string(raw_input)
                                        .unwrap_or_else(|_| "{}".to_string());
                                    if !args.is_empty() {
                                        call.function.arguments = args;
                                    }
                                }
                                msg.timestamp = std::time::SystemTime::now();
                                msg.elapsed += chunk_elapsed;
                                *self.start_time.lock().await = std::time::Instant::now();
                                found = true;
                                break;
                            }
                        }
                    }
                }

                // If not found, create new tool call in last assistant message
                if !found {
                    if let Some(last) = messages.last_mut() {
                        if let Message::Assistant(ref mut am) = last.message {
                            let arguments = tool_call
                                .raw_input
                                .as_ref()
                                .and_then(|v| serde_json::to_string(v).ok())
                                .unwrap_or_else(|| "{}".to_string());
                            let call = tiny_loop::types::ToolCall {
                                id: tool_call_id,
                                call_type: "function".to_string(),
                                function: tiny_loop::types::FunctionCall {
                                    name: title,
                                    arguments,
                                },
                            };
                            if let Some(ref mut calls) = am.tool_calls {
                                calls.push(call);
                            } else {
                                am.tool_calls = Some(vec![call]);
                            }
                            last.timestamp = std::time::SystemTime::now();
                            last.elapsed += chunk_elapsed;
                            *self.start_time.lock().await = std::time::Instant::now();
                        }
                    }
                }
            }
            // Tool call update: find existing tool call by id in assistant message and update fields
            SessionUpdate::ToolCallUpdate(update) => {
                let tool_call_id = update.tool_call_id.to_string();
                let chunk_elapsed = self.start_time.lock().await.elapsed();

                // Search backwards for existing tool call with matching id
                for msg in messages.iter_mut().rev() {
                    if let Message::Assistant(ref mut am) = msg.message {
                        if let Some(ref mut calls) = am.tool_calls {
                            if let Some(call) = calls.iter_mut().find(|c| c.id == tool_call_id) {
                                // Update title if provided and not empty
                                if let Some(title) = update.fields.title {
                                    if !title.is_empty() {
                                        call.function.name = title;
                                    }
                                }
                                // Update arguments from raw_input if provided and not empty
                                if let Some(raw_input) = update.fields.raw_input {
                                    let args = serde_json::to_string(&raw_input)
                                        .unwrap_or_else(|_| "{}".to_string());
                                    if !args.is_empty() {
                                        call.function.arguments = args;
                                    }
                                }
                                msg.timestamp = std::time::SystemTime::now();
                                msg.elapsed += chunk_elapsed;
                                *self.start_time.lock().await = std::time::Instant::now();
                                return Ok(());
                            }
                        }
                    }
                }
                *self.start_time.lock().await = std::time::Instant::now();
            }
            _ => {}
        }

        Ok(())
    }

    async fn request_permission(
        &self,
        req: agent_client_protocol::RequestPermissionRequest,
    ) -> agent_client_protocol::Result<agent_client_protocol::RequestPermissionResponse> {
        let first_option = req
            .options
            .first()
            .ok_or_else(|| agent_client_protocol::Error::invalid_params())?;

        Ok(agent_client_protocol::RequestPermissionResponse::new(
            agent_client_protocol::RequestPermissionOutcome::Selected(
                agent_client_protocol::SelectedPermissionOutcome::new(
                    first_option.option_id.clone(),
                ),
            ),
        ))
    }
}
