//! 【実機検証(手動確認)で判明した既知の制約への対応】
//! 以前はこのbackendにリクエスト単位のログが1行も無く、起動時の3行(REST/gRPC/外部公開API起動)
//! しか出ていなかった。backend(Go)のJSONログ(method/path/status/duration_ms)やbackend-rails標準の
//! Railsログと違い、実際にどのリクエストを処理したかログだけでは確認できず、
//! backend.task-languageをrustへ切り替えた動作確認がbffのログでしか裏付けできない状態だった。
//!
//! method/path/status/durationを1リクエスト1行のINFOログとして必ず出すことで、
//! `LOG_LEVEL=debug`無しでも既定で確認できるようにする。
//! tower_http::trace::TraceLayerも検討したが、on_request/on_responseの2コールバックに
//! 分かれておりmethod+path+status+durationを1行にまとめるにはstateの受け渡しが余計に必要になるため、
//! axum標準のmiddleware::from_fnで素朴に1関数にまとめた方がシンプルだった

use std::time::Instant;

use axum::extract::Request;
use axum::middleware::Next;
use axum::response::Response;

pub async fn log_requests(req: Request, next: Next) -> Response {
    let method = req.method().clone();
    let path = req.uri().path().to_string();
    let start = Instant::now();

    let response = next.run(req).await;

    let status = response.status().as_u16();
    let duration_ms = start.elapsed().as_millis();
    tracing::info!(method = %method, path = %path, status, duration_ms, "request");

    response
}

// --- gRPC(tonic)側のリクエストログ ---
//
// 【追記】当初はここにtower::Layer/Serviceで実装したGrpcLoggingLayer(HTTP/tonicの
// トランスポート層をラップし、method+durationのみログする実装)があったが、実際のgRPC
// ステータス(成否)を正確に記録できるよう、grpc/task.rsのLoggingTaskGrpcService
// (TaskServiceトレイトの各メソッドが返すResult<Response<T>, Status>をそのまま見る)に
// 置き換えた。トランスポート層ではgRPCステータスがレスポンスのHTTP/2トレーラーに乗るため
// 正確な値を取るにはbodyラップが別途必要だったが、trait実装レベルならその必要が無い
// (詳細はgrpc/task.rsのlogged()関数のコメント参照)。

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use axum::http::{Request as HttpRequest, StatusCode};
    use axum::routing::get;
    use axum::Router;
    use tower::ServiceExt;

    async fn ok_handler() -> &'static str {
        "hello"
    }

    // 【テスト監査で追加】log_requestsがステータス・ボディをラップ前後で変えていないことを確認する
    // (ミドルウェアが薄いはずが、実は誤ってレスポンスを消費/加工していないかの回帰確認)
    #[tokio::test]
    async fn log_requests_passes_through_status_and_body_unchanged() {
        let app = Router::new()
            .route("/x", get(ok_handler))
            .layer(axum::middleware::from_fn(log_requests));

        let resp = app
            .oneshot(HttpRequest::builder().uri("/x").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(resp.status(), StatusCode::OK);
        let body = axum::body::to_bytes(resp.into_body(), usize::MAX).await.unwrap();
        assert_eq!(&body[..], b"hello");
    }

    // 【テスト監査で追加、ログインジェクション対策の裏付け】
    // log_requestsはreq.uri().path()を%pathとしてそのままログに出す。もしここに生の改行が
    // 混入すると、1リクエスト1行という前提が崩れ偽の追加ログ行を注入できてしまう。
    // http::Uriのパーサ自体が制御文字(生の改行含む)を含む文字列をUriとして構築できないため、
    // このペイロードはそもそもaxumのハンドラ(延いてはlog_requests)まで到達し得ないことを確認する。
    // percent-encodeされた"%0A"はデコードされずただの文字列として残るため、その経路でも無害。
    #[test]
    fn a_raw_newline_cannot_appear_in_an_http_uri_path() {
        let result: Result<axum::http::Uri, _> = "/tasks\nFAKE LOG LINE".parse();
        assert!(
            result.is_err(),
            "改行を含む文字列がUriとしてパースできてしまっている(ログ注入の実害になり得る)"
        );

        let encoded: axum::http::Uri = "/tasks%0AFAKE".parse().expect("percent-encoded文字列は正当なUri");
        assert_eq!(
            encoded.path(),
            "/tasks%0AFAKE",
            "path()がpercent-decodeされてしまうと生の改行が紛れ込む余地が生まれる"
        );
    }
}
