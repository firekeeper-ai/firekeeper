use crate::types::Violation;
use std::sync::Arc;
use tiny_loop::tool::tool;
use tokio::sync::Mutex;

/// Tool for reporting rule violations found during code review
#[derive(Clone)]
pub struct Report {
    pub violations: Arc<Mutex<Vec<Violation>>>,
}

impl Report {
    /// Create a new Report tool
    pub fn new() -> Self {
        Self {
            violations: Arc::new(Mutex::new(Vec::new())),
        }
    }
}

#[tool]
impl Report {
    /// Report rule violations found during review.
    pub async fn report(
        self,
        /// List of violations
        violations: Vec<Violation>,
    ) -> String {
        self.violations.lock().await.extend(violations);
        "OK".into()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_report_stores_violations() {
        let report = Report::new();
        let violations = vec![Violation {
            file: "test.rs".to_string(),
            detail: "test violation".to_string(),
            start_line: 1,
            end_line: 2,
        }];

        report.violations.lock().await.extend(violations);

        let stored = report.violations.lock().await;
        assert_eq!(stored.len(), 1);
        assert_eq!(stored[0].file, "test.rs");
        assert_eq!(stored[0].detail, "test violation");
    }

    #[tokio::test]
    async fn test_report_accumulates_violations() {
        let report = Report::new();

        report.violations.lock().await.push(Violation {
            file: "a.rs".to_string(),
            detail: "first".to_string(),
            start_line: 1,
            end_line: 1,
        });

        report.violations.lock().await.push(Violation {
            file: "b.rs".to_string(),
            detail: "second".to_string(),
            start_line: 2,
            end_line: 2,
        });

        let stored = report.violations.lock().await;
        assert_eq!(stored.len(), 2);
    }
}
