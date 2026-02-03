use crate::rule::body::RuleBody;
use std::sync::Arc;
use tiny_loop::tool::tool;
use tokio::sync::Mutex;

/// Tool for suggesting new review rules based on code changes
#[derive(Clone)]
pub struct Suggest {
    pub rules: Arc<Mutex<Vec<RuleBody>>>,
}

impl Suggest {
    /// Create a new Suggest tool
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

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_suggest_stores_rules() {
        let suggest = Suggest::new();
        let rules = vec![RuleBody {
            name: "test-rule".to_string(),
            description: "test description".to_string(),
            instruction: "test instruction".to_string(),
            scope: vec!["**/*".to_string()],
            exclude: vec![],
            max_files_per_task: None,
            blocking: true,
            tip: None,
        }];

        suggest.rules.lock().await.extend(rules);

        let stored = suggest.rules.lock().await;
        assert_eq!(stored.len(), 1);
        assert_eq!(stored[0].name, "test-rule");
    }

    #[tokio::test]
    async fn test_suggest_accumulates_rules() {
        let suggest = Suggest::new();

        suggest.rules.lock().await.push(RuleBody {
            name: "rule1".to_string(),
            description: "desc1".to_string(),
            instruction: "instruction1".to_string(),
            scope: vec!["**/*".to_string()],
            exclude: vec![],
            max_files_per_task: None,
            blocking: true,
            tip: None,
        });

        suggest.rules.lock().await.push(RuleBody {
            name: "rule2".to_string(),
            description: "desc2".to_string(),
            instruction: "instruction2".to_string(),
            scope: vec!["**/*".to_string()],
            exclude: vec![],
            max_files_per_task: None,
            blocking: false,
            tip: None,
        });

        let stored = suggest.rules.lock().await;
        assert_eq!(stored.len(), 2);
    }
}
