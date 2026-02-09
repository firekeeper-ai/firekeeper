use std::process::Stdio;
use tiny_loop::tool::tool;
use tokio::io::AsyncReadExt;
use tokio::process::Command;

use super::utils::{DEFAULT_NUM_CHARS, truncate_with_hint};

const WHITELIST: &[&str] = &["ls", "cat", "grep", "find", "head", "tail", "wc"];
/// Default timeout for shell command execution
const TIMEOUT_SECS: u64 = 5;

/// Execute whitelisted shell commands safely. Allowed commands: ls, cat, grep, find, head, tail, wc
#[tool]
pub async fn sh(
    /// Shell command string (e.g., "ls -la /tmp")
    command: String,
    /// Optional start character index (default: 0)
    start_char: Option<usize>,
    /// Optional number of characters to return (default: 5000)
    num_chars: Option<usize>,
    /// Optional timeout in seconds (default: 5)
    timeout_secs: Option<u64>,
) -> String {
    let parts = match shell_words::split(&command) {
        Ok(p) => p,
        Err(e) => return format!("Failed to parse command: {}", e),
    };

    if parts.is_empty() {
        return "Error: Empty command".to_string();
    }

    let cmd = &parts[0];
    if !WHITELIST.contains(&cmd.as_str()) {
        return format!(
            "Error: Command '{}' not allowed. Allowed: {:?}",
            cmd, WHITELIST
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

    let timeout = tokio::time::sleep(tokio::time::Duration::from_secs(
        timeout_secs.unwrap_or(TIMEOUT_SECS),
    ));
    tokio::pin!(timeout);

    let output = tokio::select! {
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
            format!("Command timed out after {} seconds", timeout_secs.unwrap_or(TIMEOUT_SECS))
        }
    };

    truncate_with_hint(
        output,
        start_char.unwrap_or(0),
        num_chars.unwrap_or(DEFAULT_NUM_CHARS),
    )
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
