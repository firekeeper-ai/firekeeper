use crate::rule::body::RuleBody;
use std::sync::Arc;
use tiny_loop::tool::tool;
use tokio::sync::Mutex;

#[derive(Clone)]
pub struct Suggest {
    pub rules: Arc<Mutex<Vec<RuleBody>>>,
}

impl Suggest {
    pub fn new() -> Self {
        Self {
            rules: Arc::new(Mutex::new(Vec::new())),
        }
    }
}

#[tool]
impl Suggest {
    /// Suggest new review rules. MUST call 'think' tool first.
    pub async fn suggest(
        self,
        /// List of suggested rules
        rules: Vec<RuleBody>,
    ) -> String {
        self.rules.lock().await.extend(rules);
        "OK".into()
    }
}
