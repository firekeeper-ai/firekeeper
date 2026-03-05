use crate::{
    tool::{diff::DiffArgs, fetch::FetchArgs, report::ReportArgs, sh::execute_sh_args},
    types::Violation,
};
use axum::{
    Json, Router,
    extract::{Path, State},
    http::StatusCode,
    routing::post,
};
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;
use tracing::debug;

use crate::tool::diff::Diff;
use crate::tool::fetch;
use crate::tool::sh::ShArgs;

pub type ViolationMap = Arc<Mutex<HashMap<String, Vec<Violation>>>>;

#[derive(Clone)]
pub struct ContextServerState {
    violations: ViolationMap,
    allowed_shell_commands: Vec<String>,
    diffs: Arc<HashMap<String, String>>,
}

pub struct ContextServer {
    state: ContextServerState,
    port: u16,
}

impl ContextServer {
    pub async fn start(
        allowed_shell_commands: Vec<String>,
        diffs: HashMap<String, String>,
    ) -> anyhow::Result<Self> {
        let violations: ViolationMap = Arc::new(Mutex::new(HashMap::new()));
        let diffs = Arc::new(diffs);

        let state = ContextServerState {
            violations,
            allowed_shell_commands,
            diffs,
        };

        let app = Router::new()
            .route("/report/{worker_id}", post(handle_report))
            .route("/sh/{worker_id}", post(handle_tool_sh))
            .route("/diff/{worker_id}", post(handle_tool_diff))
            .route("/fetch/{worker_id}", post(handle_tool_fetch))
            .with_state(state.clone());

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await?;
        let port = listener.local_addr()?.port();

        tokio::spawn(async move {
            axum::serve(listener, app).await.ok();
        });

        Ok(Self { state, port })
    }

    pub fn url(&self) -> String {
        format!("http://127.0.0.1:{}", self.port)
    }

    pub async fn get_violations(&self, worker_id: &str) -> Vec<Violation> {
        self.state
            .violations
            .lock()
            .await
            .get(worker_id)
            .cloned()
            .unwrap_or_default()
    }
}

async fn handle_report(
    State(state): State<ContextServerState>,
    Path(worker_id): Path<String>,
    Json(args): Json<ReportArgs>,
) -> StatusCode {
    debug!("Received violation for worker {}: {:?}", worker_id, args);

    state
        .violations
        .lock()
        .await
        .entry(worker_id)
        .or_default()
        .extend(args.violations);

    StatusCode::OK
}

async fn handle_tool_sh(
    State(state): State<ContextServerState>,
    Path(_worker_id): Path<String>,
    Json(args): Json<ShArgs>,
) -> String {
    execute_sh_args(args, &state.allowed_shell_commands).await
}

async fn handle_tool_diff(
    State(state): State<ContextServerState>,
    Path(_worker_id): Path<String>,
    Json(req): Json<DiffArgs>,
) -> String {
    let diff = Diff::new((*state.diffs).clone());
    diff.diff(req).await
}

async fn handle_tool_fetch(
    State(_state): State<ContextServerState>,
    Path(_worker_id): Path<String>,
    Json(req): Json<FetchArgs>,
) -> String {
    fetch::fetch(req).await
}
