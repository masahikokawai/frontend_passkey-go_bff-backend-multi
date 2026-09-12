use std::sync::Arc;
use std::time::Instant;

use chrono::{NaiveDate, NaiveDateTime, Utc};
use tonic::{Request, Response, Status};

use crate::auth::{resolve_user_id, ResolveOutcome};
use crate::db;
use crate::model::{validate_task_input, Task, TaskInput, TaskStatus};
use crate::AppState;

use super::pb;
use pb::task_service_server::TaskService;

pub struct TaskGrpcService {
    pub state: Arc<AppState>,
}

fn naive_to_timestamp(dt: NaiveDateTime) -> prost_types::Timestamp {
    let utc = dt.and_utc();
    prost_types::Timestamp { seconds: utc.timestamp(), nanos: utc.timestamp_subsec_nanos() as i32 }
}

fn task_to_pb(task: &Task) -> pb::Task {
    pb::Task {
        id: task.id,
        name: task.name.clone(),
        description: task.description.clone(),
        status: task.status.as_str().to_string(),
        finished_on: task.finished_on.format("%Y-%m-%d").to_string(),
        labels: task
            .labels
            .iter()
            .map(|l| pb::Label { id: l.id, name: l.name.clone() })
            .collect(),
        created_at: Some(naive_to_timestamp(task.created_at)),
        updated_at: Some(naive_to_timestamp(task.updated_at)),
    }
}

/// backend(Go)のTaskServer.resolveUserID(gRPC v2版)と同じ:
/// メタデータのauthorizationを検証し、未検証/未プロビジョニングをgRPCステータスへ変換する
async fn authenticate<T>(state: &AppState, req: &Request<T>) -> Result<u64, Status> {
    let auth_header = req
        .metadata()
        .get("authorization")
        .and_then(|v| v.to_str().ok())
        .ok_or_else(|| Status::unauthenticated("unauthorized"))?;
    let token = auth_header
        .strip_prefix("Bearer ")
        .ok_or_else(|| Status::unauthenticated("unauthorized"))?;

    let claims = state
        .dispatcher
        .verify(token)
        .await
        .map_err(|_| Status::unauthenticated("unauthorized"))?;

    match resolve_user_id(&state.pool, &claims).await {
        ResolveOutcome::Ok(user_id) => Ok(user_id),
        ResolveOutcome::UserNotProvisioned => Err(Status::permission_denied("user not provisioned")),
    }
}

fn parse_finished_on(s: &str) -> Result<NaiveDate, Status> {
    NaiveDate::parse_from_str(s, "%Y-%m-%d").map_err(|e| Status::invalid_argument(format!("invalid finished_on: {}", e)))
}

fn build_input(name: String, description: Option<String>, status: String, finished_on: &str, label_ids: Vec<u64>) -> Result<(TaskInput, NaiveDate), Status> {
    let parsed = parse_finished_on(finished_on)?;
    Ok((TaskInput { name, description, status, finished_on: parsed, label_ids }, parsed))
}

fn validate(input: &TaskInput) -> Result<TaskStatus, Status> {
    let today = Utc::now().date_naive();
    validate_task_input(input, today).map_err(Status::invalid_argument)
}

#[tonic::async_trait]
impl TaskService for TaskGrpcService {
    async fn list_tasks(
        &self,
        request: Request<pb::ListTasksRequest>,
    ) -> Result<Response<pb::ListTasksResponse>, Status> {
        let user_id = authenticate(&self.state, &request).await?;
        let req = request.into_inner();

        let status = if req.status.is_empty() {
            None
        } else {
            Some(TaskStatus::from_str(&req.status).ok_or_else(|| Status::invalid_argument(format!("invalid status: {}", req.status)))?)
        };
        let limit = if req.limit <= 0 { 20 } else { req.limit as i64 };

        let tasks = db::list_tasks_cursor(&self.state.pool, user_id, &req.name, status, &req.label_ids, req.cursor, limit)
            .await
            .map_err(|e| Status::internal(format!("list tasks: {}", e)))?;

        let next_cursor = if (tasks.len() as i64) < limit {
            0
        } else {
            tasks.last().map(|t| t.id).unwrap_or(0)
        };

        Ok(Response::new(pb::ListTasksResponse {
            tasks: tasks.iter().map(task_to_pb).collect(),
            next_cursor,
        }))
    }

    async fn get_task(&self, request: Request<pb::GetTaskRequest>) -> Result<Response<pb::Task>, Status> {
        let user_id = authenticate(&self.state, &request).await?;
        let id = request.into_inner().id;
        let task = db::get_task(&self.state.pool, id, user_id)
            .await
            .map_err(|e| Status::internal(e.to_string()))?
            .ok_or_else(|| Status::not_found("task not found"))?;
        Ok(Response::new(task_to_pb(&task)))
    }

    async fn create_task(&self, request: Request<pb::CreateTaskRequest>) -> Result<Response<pb::Task>, Status> {
        let user_id = authenticate(&self.state, &request).await?;
        let req = request.into_inner();
        let (input, _) = build_input(req.name, req.description, req.status, &req.finished_on, req.label_ids)?;
        let status = validate(&input)?;

        let id = db::create_task(&self.state.pool, user_id, &input, status)
            .await
            .map_err(|e| Status::internal(e.to_string()))?;
        let task = db::get_task(&self.state.pool, id, user_id)
            .await
            .map_err(|e| Status::internal(e.to_string()))?
            .ok_or_else(|| Status::internal("task disappeared after create"))?;
        Ok(Response::new(task_to_pb(&task)))
    }

    async fn update_task(&self, request: Request<pb::UpdateTaskRequest>) -> Result<Response<pb::Task>, Status> {
        let user_id = authenticate(&self.state, &request).await?;
        let req = request.into_inner();
        let (input, _) = build_input(req.name, req.description, req.status, &req.finished_on, req.label_ids)?;
        let status = validate(&input)?;

        let updated = db::update_task(&self.state.pool, req.id, user_id, &input, status)
            .await
            .map_err(|e| Status::internal(e.to_string()))?;
        if !updated {
            return Err(Status::not_found("task not found"));
        }
        let task = db::get_task(&self.state.pool, req.id, user_id)
            .await
            .map_err(|e| Status::internal(e.to_string()))?
            .ok_or_else(|| Status::not_found("task not found"))?;
        Ok(Response::new(task_to_pb(&task)))
    }

    async fn delete_task(&self, request: Request<pb::DeleteTaskRequest>) -> Result<Response<pb::DeleteTaskResponse>, Status> {
        let user_id = authenticate(&self.state, &request).await?;
        let id = request.into_inner().id;
        let deleted = db::delete_task(&self.state.pool, id, user_id)
            .await
            .map_err(|e| Status::internal(e.to_string()))?;
        if !deleted {
            return Err(Status::not_found("task not found"));
        }
        Ok(Response::new(pb::DeleteTaskResponse {}))
    }
}

/// method/実際のgRPCステータス(Status::code())/durationを1rpc1行のINFOログとして出す
///
/// 【実機検証で判明・修正】以前は`GrpcLoggingLayer`(tower::Layer、HTTP/tonicのトランスポート層を
/// ラップする実装)でmethod+durationだけログしていたが、実際のgRPCステータス(成否)はレスポンスの
/// HTTP/2トレーラーに乗るため、トランスポート層からは素直に取れなかった(トレーラー到達を検知する
/// bodyラップが別途必要になる)。
///
/// ここではその代わりに、TaskServiceトレイトの各メソッドが返す`Result<Response<T>, Status>`を
/// そのまま見る(Goの`grpc.ChainUnaryInterceptor`がhandlerの`err`をそのまま受け取れるのと同じ発想、
/// Railsの`GRPC::BadStatus#code`と同じ発想)。この層は既にビジネスロジックの戻り値そのものであり、
/// トレーラーを待つ必要が無いため、実際のステータスコードを正確に記録できる
async fn logged<T>(method: &str, fut: impl std::future::Future<Output = Result<Response<T>, Status>>) -> Result<Response<T>, Status> {
    let start = Instant::now();
    let result = fut.await;
    let duration_ms = start.elapsed().as_millis();
    let code = match &result {
        Ok(_) => tonic::Code::Ok,
        Err(status) => status.code(),
    };
    tracing::info!(method, status = ?code, duration_ms, "grpc request");
    result
}

/// TaskGrpcServiceを包み、全rpcにlogged()を通す薄いデコレータ
/// (Rustにはモンキーパッチが無いため、5メソッド分のログをtrait実装レベルで一箇所にまとめる手段として
/// デコレータ構造体を使う。Railsの`Module#prepend`・Goのinterceptorチェーンと同じ役割)
pub struct LoggingTaskGrpcService {
    pub inner: TaskGrpcService,
}

#[tonic::async_trait]
impl TaskService for LoggingTaskGrpcService {
    async fn list_tasks(
        &self,
        request: Request<pb::ListTasksRequest>,
    ) -> Result<Response<pb::ListTasksResponse>, Status> {
        logged("list_tasks", self.inner.list_tasks(request)).await
    }

    async fn get_task(&self, request: Request<pb::GetTaskRequest>) -> Result<Response<pb::Task>, Status> {
        logged("get_task", self.inner.get_task(request)).await
    }

    async fn create_task(&self, request: Request<pb::CreateTaskRequest>) -> Result<Response<pb::Task>, Status> {
        logged("create_task", self.inner.create_task(request)).await
    }

    async fn update_task(&self, request: Request<pb::UpdateTaskRequest>) -> Result<Response<pb::Task>, Status> {
        logged("update_task", self.inner.update_task(request)).await
    }

    async fn delete_task(&self, request: Request<pb::DeleteTaskRequest>) -> Result<Response<pb::DeleteTaskResponse>, Status> {
        logged("delete_task", self.inner.delete_task(request)).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // 【テスト監査で追加】LoggingTaskGrpcServiceの5メソッドは全てlogged()を経由するだけの
    // 薄いラップなので、logged()自体が「戻り値・エラーをそのまま透過させる」ことさえ
    // 保証できれば、実質的にデコレータ全体の正しさを裏付けられる(5メソッド分の重複テスト
    // を書かずに済む)。以前は grpc/task.rs にテストが1件も無く、
    // LoggingTaskGrpcService(main.rsで実際に使われる方)を経由する経路が
    // 一度も検証されていなかった。
    #[tokio::test]
    async fn logged_passes_through_ok_response_unchanged() {
        let result = logged("list_tasks", async { Ok(Response::new(42u32)) }).await;
        let inner = result.expect("Okがloggingを通してErrに化けている").into_inner();
        assert_eq!(inner, 42, "レスポンスの中身がラップ前後で変わっている");
    }

    #[tokio::test]
    async fn logged_passes_through_error_status_unchanged() {
        // 実際のハンドラ(get_task等)がNotFoundを返すケースを模す
        let result: Result<Response<()>, Status> =
            logged("get_task", async { Err(Status::not_found("task not found")) }).await;
        let status = result.expect_err("Errがloggingを通してOkに化けている(呼び出し元がエラーを検知できなくなる実害)");
        assert_eq!(status.code(), tonic::Code::NotFound, "ステータスコードが変わっている");
        assert_eq!(status.message(), "task not found", "エラーメッセージが変わっている");
    }

    #[tokio::test]
    async fn logged_does_not_panic_for_any_status_code() {
        // status = ?code (Debugフォーマット)が、tonic::Codeの全バリアントで
        // panicせず記録できることを確認する(将来tonicがバリアントを追加した場合の
        // 回帰検知も兼ねる)
        let codes = [
            tonic::Code::Cancelled,
            tonic::Code::Unknown,
            tonic::Code::InvalidArgument,
            tonic::Code::DeadlineExceeded,
            tonic::Code::NotFound,
            tonic::Code::AlreadyExists,
            tonic::Code::PermissionDenied,
            tonic::Code::ResourceExhausted,
            tonic::Code::FailedPrecondition,
            tonic::Code::Aborted,
            tonic::Code::OutOfRange,
            tonic::Code::Unimplemented,
            tonic::Code::Internal,
            tonic::Code::Unavailable,
            tonic::Code::DataLoss,
            tonic::Code::Unauthenticated,
        ];
        for code in codes {
            let result: Result<Response<()>, Status> =
                logged("x", async { Err(Status::new(code, "msg")) }).await;
            assert_eq!(result.unwrap_err().code(), code);
        }
    }
}
