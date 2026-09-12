//! backend-rust: CONTRACT.mdセクション20のTask CRUD Rust実装(Axum + tonic)。
//! backend(Go)と同一のワイヤー契約(REST JSON形状・gRPC .proto)を持つことが最重要の制約。
//! 1プロセスでREST(Axum)とgRPC(tonic)を両方起動する構成もbackend(Go)のcmd/server/main.goに倣った。
//! 実体はsrc/lib.rsのモジュール群(tests/から直接使えるようlibへ切り出している)。

use std::sync::Arc;

use tonic::transport::Server as GrpcServer;

use backend_rust::auth::jwt::{Dispatcher, HmacVerifier, JwksVerifier, LOCAL_HMAC_ISSUER, LOCAL_RSA_ISSUER};
use backend_rust::external::ExternalState;
use backend_rust::flags::FlagCache;
use backend_rust::{config, db, external, grpc, rest, AppState};

fn parse_addr(addr: &str) -> String {
    // Go実装の既定値は ":8093" のような形式(host省略)。Rust側はSocketAddrに
    // hostが必須なため、先頭が':'なら0.0.0.0を補う
    if let Some(port) = addr.strip_prefix(':') {
        format!("0.0.0.0:{}", port)
    } else {
        addr.to_string()
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let cfg = config::Config::load();

    let filter = tracing_subscriber::EnvFilter::try_new(&cfg.log_level)
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info"));
    tracing_subscriber::fmt().with_env_filter(filter).init();

    tracing::info!(dsn = %cfg.db_dsn, "MySQLへ接続します");
    let pool = db::connect(&cfg.db_dsn).await?;

    // backend(Go)のcmd/server/main.goと同じ3issuer構成
    // (Keycloak/ローカルHMAC/ローカルRSA)をDispatcherへ登録する
    let keycloak_verifier = JwksVerifier::new(cfg.keycloak_jwks_url.clone(), cfg.keycloak_issuer.clone(), cfg.expected_audience.clone());
    let local_hmac_verifier = HmacVerifier::new(cfg.local_hmac_secret.clone(), LOCAL_HMAC_ISSUER, cfg.expected_audience.clone());
    let local_rsa_verifier = JwksVerifier::new(cfg.local_rsa_jwks_url.clone(), LOCAL_RSA_ISSUER, cfg.expected_audience.clone());

    let dispatcher = Dispatcher::new()
        .register(cfg.keycloak_issuer.clone(), Box::new(keycloak_verifier))
        .register(LOCAL_HMAC_ISSUER, Box::new(local_hmac_verifier))
        .register(LOCAL_RSA_ISSUER, Box::new(local_rsa_verifier));

    // 外部公開API(CONTRACT.mdセクション11)専用: Keycloak発行のClient Credentials Grant
    // トークンしか扱わないため、内部CRUD用の3issuer Dispatcherとは別にKeycloak向けの
    // JwksVerifier単体を用意する
    let external_keycloak_verifier =
        JwksVerifier::new(cfg.keycloak_jwks_url.clone(), cfg.keycloak_issuer.clone(), cfg.expected_audience.clone());
    let pagination_v2 = FlagCache::spawn(
        pool.clone(),
        "backend.external-tasks-pagination-v2",
        std::time::Duration::from_secs(cfg.feature_flag_poll_interval_secs),
    )
    .await;
    let external_state = Arc::new(ExternalState {
        pool: pool.clone(),
        keycloak_verifier: external_keycloak_verifier,
        expected_client_id: cfg.external_api_client_id.clone(),
        pagination_v2,
    });

    let state = Arc::new(AppState { pool, dispatcher });

    let http_addr = parse_addr(&cfg.http_addr);
    let grpc_addr = parse_addr(&cfg.grpc_addr);
    let external_http_addr = parse_addr(&cfg.external_http_addr);

    let rest_state = state.clone();
    let rest_task = tokio::spawn(async move {
        let app = rest::router(rest_state);
        let listener = tokio::net::TcpListener::bind(&http_addr).await.expect("REST待受アドレスのbindに失敗");
        tracing::info!(addr = %http_addr, "REST(Axum)起動");
        axum::serve(listener, app).await.expect("Axum server error");
    });

    let grpc_state = state.clone();
    let grpc_task = tokio::spawn(async move {
        let addr = grpc_addr.parse().expect("gRPC待受アドレスのパースに失敗");
        // LoggingTaskGrpcServiceが実際のgRPCステータスまで含めてログするため
        // (grpc/task.rs参照)、トランスポート層のGrpcLoggingLayerは不要になり撤去した
        let svc = grpc::task::LoggingTaskGrpcService { inner: grpc::task::TaskGrpcService { state: grpc_state } };
        tracing::info!(%addr, "gRPC(tonic)起動");
        GrpcServer::builder()
            .add_service(grpc::pb::task_service_server::TaskServiceServer::new(svc))
            .serve(addr)
            .await
            .expect("tonic server error");
    });

    let external_task = tokio::spawn(async move {
        let app = external::router(external_state);
        let listener = tokio::net::TcpListener::bind(&external_http_addr)
            .await
            .expect("外部公開API待受アドレスのbindに失敗");
        tracing::info!(addr = %external_http_addr, "外部公開API(Axum)起動");
        axum::serve(listener, app).await.expect("Axum server error");
    });

    let _ = tokio::join!(rest_task, grpc_task, external_task);
    Ok(())
}
