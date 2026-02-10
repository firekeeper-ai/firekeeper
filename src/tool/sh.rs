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
    /// Shell command string (e.g., "ls -la /tmp")
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
                "Execute an allowlisted shell command.\nAllowed commands: {}.\nRedirections are not allowed.",
                commands_str
            ),
            parameters: Parameters::from_type::<ShArgs>(),
        },
    }
}

pub async fn execute_sh_args(args: ShArgs, allowed_commands: &[String]) -> String {
    let result = execute_sh_raw(
        args.command,
        args.timeout_secs.unwrap_or(DEFAULT_TIMEOUT_SECS),
        allowed_commands,
    )
    .await;
    truncate_with_hint(
        result,
        args.start_char.unwrap_or(0),
        args.num_chars.unwrap_or(DEFAULT_NUM_CHARS),
    )
}

pub async fn execute_sh_raw(
    command: String,
    timeout_secs: u64,
    allowed_commands: &[String],
) -> String {
    let parts = match shell_words::split(&command) {
        Ok(p) => p,
        Err(e) => return format!("Failed to parse command: {}", e),
    };

    if parts.is_empty() {
        return "Error: Empty command".to_string();
    }

    let cmd = &parts[0];
    if !allowed_commands.iter().any(|c| c == cmd) {
        return format!(
            "Error: Command '{}' not allowed. Allowed: {:?}",
            cmd, allowed_commands
        );
    }

    let mut child = match Command::new(cmd)
        .args(&parts[1..])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
    {
        Ok(c) => c,
        Err(e) => return format!("Failed to execute command: {}", e),
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
                        format!("Command failed with status {}\nstdout:\n{}\nstderr:\n{}", status, stdout, stderr)
                    } else if !stderr.is_empty() {
                        format!("{}\nstderr:\n{}", stdout, stderr)
                    } else {
                        stdout
                    }
                }
                Err(e) => format!("Failed to wait for command: {}", e),
            }
        }
        _ = &mut timeout => {
            let _ = child.kill().await;
            format!("Command timed out after {} seconds", timeout_secs)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_truncate_output_no_truncation() {
        let result = truncate_with_hint("hello".to_string(), 0, 100);
        assert_eq!(result, "hello");
    }

    #[test]
    fn test_truncate_output_with_truncation() {
        let content = "a".repeat(100);
        let result = truncate_with_hint(content, 0, 50);
        assert!(result.contains("Hint: Use start_char=50 to read more."));
    }

    #[test]
    fn test_truncate_output_with_start() {
        let result = truncate_with_hint("0123456789".to_string(), 5, 100);
        assert_eq!(result, "56789");
    }
}
