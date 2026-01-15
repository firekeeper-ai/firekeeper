use crate::rule::body::RuleBody;

pub async fn worker(rule: &RuleBody, files: Vec<String>) {
    println!("Worker: reviewing {} files for rule '{}': {:?}", files.len(), rule.name, files);
    tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
}
