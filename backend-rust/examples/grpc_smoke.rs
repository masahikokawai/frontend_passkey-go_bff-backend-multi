//! 手動動作確認用: gRPC(:9093)へ実際に接続し、ListTasks/CreateTask/DeleteTaskを1回ずつ叩く
//! 使い方: TOKEN環境変数にローカルHMAC発行のBearerトークン(sub=既存user_id)を入れて
//!   cargo run --example grpc_smoke

use backend_rust::grpc::pb::task_service_client::TaskServiceClient;
use backend_rust::grpc::pb::{CreateTaskRequest, DeleteTaskRequest, ListTasksRequest};
use tonic::Request;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let token = std::env::var("TOKEN").expect("TOKEN env var required (bearer token)");
    let addr = std::env::var("GRPC_ADDR").unwrap_or_else(|_| "http://127.0.0.1:9093".to_string());

    let mut client = TaskServiceClient::connect(addr).await?;

    let mut req = Request::new(ListTasksRequest::default());
    req.metadata_mut().insert("authorization", format!("Bearer {}", token).parse()?);
    let resp = client.list_tasks(req).await?;
    println!("ListTasks OK: {} task(s), next_cursor={}", resp.get_ref().tasks.len(), resp.get_ref().next_cursor);

    let mut req = Request::new(CreateTaskRequest {
        name: "grpc-smoke".to_string(),
        description: None,
        status: "waiting".to_string(),
        finished_on: "2030-01-01".to_string(),
        label_ids: vec![],
    });
    req.metadata_mut().insert("authorization", format!("Bearer {}", token).parse()?);
    let created = client.create_task(req).await?.into_inner();
    println!("CreateTask OK: id={} name={}", created.id, created.name);

    let mut req = Request::new(DeleteTaskRequest { id: created.id });
    req.metadata_mut().insert("authorization", format!("Bearer {}", token).parse()?);
    client.delete_task(req).await?;
    println!("DeleteTask OK: id={}", created.id);

    Ok(())
}
