use schemars::JsonSchema;
use serde::{Deserialize, Serialize};
use std::process::Stdio;
use tiny_loop::types::{Parameters, ToolDefinition, ToolFunction};
use tokio::io::AsyncReadExt;
use tokio::process::Command;

use super::utils::{DEFAULT_NUM_CHARS, truncate_with_hint};

/// Default timeout for shell command execution in seconds
const DEFAULT_TIMEOUT_SECS: u64 = 5;

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
    /// Optional timeout in seconds (default: 5)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timeout_secs: Option<u64>,
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
    Timeout(u64),
}

impl std::fmt::Display for ShError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ShError::ValidationError(e) => write!(f, "Command validation failed: {}", e),
            ShError::ExecutionError(e) => write!(f, "Failed to execute command: {}", e),
            ShError::Timeout(secs) => write!(f, "Command timed out after {} seconds", secs),
        }
    }
}

pub async fn execute_sh_args(args: ShArgs, allowed_commands: &[String]) -> String {
    match execute_sh_raw(
        args.command,
        args.timeout_secs.unwrap_or(DEFAULT_TIMEOUT_SECS),
        allowed_commands,
    )
    .await
    {
        Ok(result) => truncate_with_hint(
            result,
            args.start_char.unwrap_or(0),
            args.num_chars.unwrap_or(DEFAULT_NUM_CHARS),
        ),
        Err(e) => e.to_string(),
    }
}

pub async fn execute_shell_command(command: &str, timeout_secs: u64) -> Result<String, ShError> {
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

    let timeout = tokio::time::sleep(tokio::time::Duration::from_secs(timeout_secs));
    tokio::pin!(timeout);

    tokio::select! {
        result = child.wait() => {
            match result {
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
                        Ok(format!("Command failed with status {}\nstdout:\n{}\nstderr:\n{}", status, stdout, stderr))
                    } else if !stderr.is_empty() {
                        Ok(format!("{}\nstderr:\n{}", stdout, stderr))
                    } else {
                        Ok(stdout)
                    }
                }
                Err(e) => Err(ShError::ExecutionError(format!("Failed to wait for command: {}", e))),
            }
        }
        _ = &mut timeout => {
            let _ = child.kill().await;
            Err(ShError::Timeout(timeout_secs))
        }
    }
}

pub async fn execute_shell_command_no_timeout(command: &str) -> Result<String, ShError> {
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
    timeout_secs: u64,
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

    execute_shell_command(&command, timeout_secs).await
}
