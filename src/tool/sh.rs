use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use std::process::Stdio;
use tiny_loop::types::{Parameters, ToolDefinition, ToolFunction};
use tokio::io::AsyncReadExt;
use tokio::process::Command;

use super::utils::{DEFAULT_NUM_CHARS, truncate_with_hint};

#[derive(Debug, Serialize, Deserialize, JsonSchema)]
pub struct ShArgs {
    /// Shell command string (e.g., `ls -la /tmp | grep foo`)
    pub command: String,
    /// Optional start character index (default: 0)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub start_char: Option<usize>,
    /// Optional number of characters to return (default: 5000)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub num_chars: Option<usize>,
}

impl ShArgs {
    pub const TOOL_NAME: &'static str = "sh";
}

pub fn sh_tool_def(allowed_commands: &[String]) -> ToolDefinition {
    let commands_str = allowed_commands.join(", ");
    ToolDefinition {
        tool_type: "function".into(),
        function: ToolFunction {
            name: ShArgs::TOOL_NAME.into(),
            description: format!(
                "Execute an allowlisted shell command. Supports pipes and redirections.\nAllowed commands: {}.",
                commands_str
            ),
            parameters: Parameters::from_type::<ShArgs>(),
        },
    }
}

#[derive(Debug)]
pub(crate) enum ShError {
    ValidationError(sheath::Error),
    ExecutionError(String),
}

impl std::fmt::Display for ShError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ShError::ValidationError(e) => write!(f, "Command validation failed: {}", e),
            ShError::ExecutionError(e) => write!(f, "Failed to execute command: {}", e),
        }
    }
}

pub async fn execute_sh_args(args: ShArgs, allowed_commands: &[String]) -> String {
    match execute_sh_raw(args.command, allowed_commands).await {
        Ok(result) => truncate_with_hint(
            result,
            args.start_char.unwrap_or(0),
            args.num_chars.unwrap_or(DEFAULT_NUM_CHARS),
        ),
        Err(e) => e.to_string(),
    }
}

pub async fn execute_shell_command(command: &str) -> Result<String, ShError> {
    let mut child = if cfg!(windows) {
        Command::new("powershell")
            .arg("-Command")
            .arg(command)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| ShError::ExecutionError(e.to_string()))?
    } else {
        Command::new("sh")
            .arg("-c")
            .arg(command)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| ShError::ExecutionError(e.to_string()))?
    };

    match child.wait().await {
        Ok(status) => {
            let mut stdout = String::new();
            let mut stderr = String::new();

            if let Some(mut out) = child.stdout.take() {
                let _ = out.read_to_string(&mut stdout).await;
            }
            if let Some(mut err) = child.stderr.take() {
                let _ = err.read_to_string(&mut stderr).await;
            }

            if !status.success() {
                Ok(format!(
                    "Command failed with status {}\nstdout:\n{}\nstderr:\n{}",
                    status, stdout, stderr
                ))
            } else if !stderr.is_empty() {
                Ok(format!("{}\nstderr:\n{}", stdout, stderr))
            } else {
                Ok(stdout)
            }
        }
        Err(e) => Err(ShError::ExecutionError(format!(
            "Failed to wait for command: {}",
            e
        ))),
    }
}

pub async fn execute_sh_raw(
    command: String,
    allowed_commands: &[String],
) -> Result<String, ShError> {
    let validator = if cfg!(windows) {
        sheath::Validator::new()
            .shell(sheath::Shell::PowerShell)
            .allow(allowed_commands.iter().map(|s| s.as_str()))
    } else {
        sheath::Validator::new().allow(allowed_commands.iter().map(|s| s.as_str()))
    };

    validator
        .validate(&command)
        .map_err(ShError::ValidationError)?;

    execute_shell_command(&command).await
}
