package com.bffgin.backendpekko

import org.apache.pekko.http.scaladsl.server.{Route, RouteResult}
import org.slf4j.Logger

// backend/internal/handler/v1/router.go の requestLogger、backend-rails の標準Railsログ
// (Started/Processing/Completed)と同じ目的: 全リクエストをmethod/path/status/duration_msでログする。
//
// 【実機検証(backend.task-languageをscala-pekkoへ切り替えての動作確認)で判明した既知の制約の是正】
// このbackendには元々リクエスト単位のログが1行も存在せず(起動時の3行のみ)、
// bffのログ(振り分け結果)以外に「このbackendが実際にどのリクエストを処理したか」を
// 確認する手段が一切無かった(Go実装・Rails実装には標準でリクエスト単位のログがあり、
// この言語だけ手薄だった。frontend-rails/without-bffへのパスキー追加作業の流れで
// ユーザーから指摘を受け、追加した)。
//
// Authorizationヘッダ(bearerトークン)やリクエスト/レスポンスボディ(タスク名等の
// 個人情報を含みうる)はログに含めず、Goのrequest loggerと同じ最小限の項目に揃える。
//
// RouteはPekko HTTPでは`RequestContext => Future[RouteResult]`という素の関数型なので、
// Directive DSL(mapResponse等)を経由せず直接ラップする方が型・タイミング計測ともに単純になる。
//
// 【既知の制約】ここでラップしているのはDirectiveレベルで完結した(=いずれかのルートに
// マッチした)リクエストのみ。完全に未マッチなパス(存在しないエンドポイント)は
// RouteResult.Rejectedのまま上位のバインド処理でエラーレスポンスへ変換されるため、
// ここでは実際のステータスコードではなく"rejected"というラベルで記録する
// (Route.sealを併用すれば解決できるが、暗黙のRoutingSettings解決が絡み変更範囲が
// 広がるため、今回のスコープ(実際に処理したリクエストを確認できるようにする)では見送った)
object RequestLogging {
  def apply(logger: Logger)(route: Route): Route = { ctx =>
    val start = System.nanoTime()
    val method = ctx.request.method.value
    val path = ctx.request.uri.path.toString
    route(ctx).map { result =>
      val durationMs = (System.nanoTime() - start) / 1000000L
      val status = result match {
        case RouteResult.Complete(response) => response.status.intValue().toString
        case RouteResult.Rejected(_)        => "rejected"
      }
      logger.info(s"method=$method path=$path status=$status duration_ms=$durationMs")
      result
    }(ctx.executionContext)
  }
}
