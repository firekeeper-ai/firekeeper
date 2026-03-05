use crate::tool::{diff::DiffArgs, fetch::FetchArgs, report::ReportArgs, sh::ShArgs};
use rmcp::{
    ServerHandler,
    handler::server::{router::tool::ToolRouter, wrapper::Parameters},
    model::*,
    tool, tool_handler, tool_router,
};
use std::borrow::Cow;
use tracing::debug;

// JSON-RPC error code for internal errors
const JSONRPC_INTERNAL_ERROR: i32 = -32603;

#[derive(Debug, Clone)]
pub struct FirekeeperMcp {
    worker_id: String,
    context_server_url: String,
    tool_router: ToolRouter<Self>,
}

#[tool_router]
impl FirekeeperMcp {
    pub fn new(worker_id: String, context_server_url: String) -> anyhow::Result<Self> {
        debug!("MCP server started for worker {}", worker_id);
        debug!("Context server: {}", context_server_url);

        Ok(Self {
            worker_id,
            context_server_url,
            tool_router: Self::tool_router(),
        })
    }

    async fn call_remote<T: serde::Serialize>(
        &self,
        endpoint: &str,
        params: &T,
        error_prefix: &str,
    ) -> Result<String, ErrorData> {
        let url = format!(
            "{}/{}/{}",
            self.context_server_url, endpoint, self.worker_id
        );
        let resp = reqwest::Client::new()
            .post(&url)
            .json(params)
            .send()
            .await
            .map_err(|e| ErrorData {
                code: ErrorCode(JSONRPC_INTERNAL_ERROR),
                message: Cow::from(format!("Network error: {}", e)),
                data: None,
            })?;

        if resp.status().is_success() {
            Ok(resp.text().await.unwrap_or_default())
        } else {
            Err(ErrorData {
                code: ErrorCode(JSONRPC_INTERNAL_ERROR),
                message: Cow::from(format!("{}: {}", error_prefix, resp.status())),
                data: None,
            })
        }
    }

    #[tool(description = "Report rule violations found during review.")]
    async fn report(
        &self,
        Parameters(params): Parameters<ReportArgs>,
    ) -> Result<CallToolResult, ErrorData> {
        self.call_remote("report", &params, "Report failed").await?;
        debug!("Reported violation for worker {}", self.worker_id);
        Ok(CallToolResult::success(vec![Content::text("success")]))
    }

    #[tool(description = "Execute a shell command")]
    async fn sh(
        &self,
        Parameters(params): Parameters<ShArgs>,
    ) -> Result<CallToolResult, ErrorData> {
        let output = self
            .call_remote("sh", &params, "Shell command failed")
            .await?;
        Ok(CallToolResult::success(vec![Content::text(output)]))
    }

    #[tool(description = "Get git diff for files")]
    async fn diff(
        &self,
        Parameters(params): Parameters<DiffArgs>,
    ) -> Result<CallToolResult, ErrorData> {
        let output = self.call_remote("diff", &params, "Diff failed").await?;
        Ok(CallToolResult::success(vec![Content::text(output)]))
    }

    #[tool(description = "Fetch webpages and convert HTML to Markdown")]
    async fn fetch(
        &self,
        Parameters(params): Parameters<FetchArgs>,
    ) -> Result<CallToolResult, ErrorData> {
        let output = self.call_remote("fetch", &params, "Fetch failed").await?;
        Ok(CallToolResult::success(vec![Content::text(output)]))
    }
}

#[tool_handler]
impl ServerHandler for FirekeeperMcp {
    fn get_info(&self) -> ServerInfo {
        let mut info = ServerInfo::new(ServerCapabilities::builder().enable_tools().build());
        info.server_info = Implementation::from_build_env();
        info.instructions = Some("Firekeeper MCP server for code review agents".into());
        info
    }
}

pub async fn run_mcp_server(args: &crate::cli::WorkerMcpArgs) -> anyhow::Result<()> {
    use rmcp::{ServiceExt, transport::stdio};

    let service = FirekeeperMcp::new(args.worker_id.clone(), args.context_server.clone())?
        .serve(stdio())
        .await?;
    service.waiting().await?;
    Ok(())
}
