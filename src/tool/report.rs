use crate::types::Violation;
use std::sync::Arc;
use tiny_loop::tool::tool;
use tokio::sync::Mutex;

#[derive(Clone)]
pub struct Report {
    pub violations: Arc<Mutex<Vec<Violation>>>,
}

impl Report {
    pub fn new() -> Self {
        Self {
            violations: Arc::new(Mutex::new(Vec::new())),
        }
    }
}

#[tool]
impl Report {
    /// Report rule violations found during review. MUST call 'think' tool first.
    pub async fn report(
        self,
        /// List of violations
        violations: Vec<Violation>,
    ) -> String {
        self.violations.lock().await.extend(violations);
        "OK".into()
    }
}
