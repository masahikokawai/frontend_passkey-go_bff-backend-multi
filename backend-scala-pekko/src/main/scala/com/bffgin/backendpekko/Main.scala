package com.bffgin.backendpekko

import com.bffgin.backendpekko.auth._
import com.bffgin.backendpekko.external.ExternalTaskRoutes
import com.bffgin.backendpekko.grpc.TaskGrpcServiceImpl
import com.bffgin.backendpekko.rest.TaskRoutes
import org.apache.pekko.actor.typed.ActorSystem
import org.apache.pekko.actor.typed.scaladsl.Behaviors
import org.apache.pekko.actor.typed.scaladsl.adapter._
import org.apache.pekko.http.scaladsl.Http
import org.slf4j.LoggerFactory
import slick.jdbc.MySQLProfile.api._
import task.v1.TaskServicePowerApiHandler

import scala.concurrent.ExecutionContext
import scala.util.{Failure, Success}

// backend/cmd/server/main.go に対応。1プロセス内でREST(Pekko HTTP, :8095既定)とgRPC(pekko-grpc,
// :9095既定)を両方起動し、DBコネクションプールとTaskServiceを共有する構成もGoと同じにしている
object Main {
  def main(args: Array[String]): Unit = {
    val logger = LoggerFactory.getLogger("Main")
    val cfg = Config.load()

    implicit val system: ActorSystem[Nothing] = ActorSystem(Behaviors.empty, "backend-scala-pekko")
    implicit val ec: ExecutionContext = system.executionContext

    val db = Database.forURL(
      url = s"jdbc:mysql://${cfg.dbDsnHost}:${cfg.dbDsnPort}/${cfg.dbDsnDatabase}?serverTimezone=UTC",
      user = "root",
      password = "",
      driver = "com.mysql.cj.jdbc.Driver"
    )

    val taskRepo = new SlickTaskRepository(db)
    val userRepo = new SlickUserRepository(db)
    val taskService = new TaskService(taskRepo, userRepo)

    val dispatcher = new JwtDispatcher(
      Map(
        cfg.keycloakIssuer -> new JwksVerifier(cfg.keycloakJwksUrl, cfg.keycloakIssuer, cfg.expectedAudience),
        JwtIssuers.LocalHmac -> new HmacVerifier(cfg.localHmacSecret, JwtIssuers.LocalHmac, cfg.expectedAudience),
        JwtIssuers.LocalRsa -> new JwksVerifier(cfg.localRsaJwksUrl, JwtIssuers.LocalRsa, cfg.expectedAudience)
      )
    )

    val restRoutes = RequestLogging(LoggerFactory.getLogger("http.rest"))(new TaskRoutes(taskService, dispatcher).routes)
    // 【ベストプラクティスの見直しで変更】以前はHTTPトランスポート層(このPartialFunction自体)を
    // ラップしてmethod/path/durationのみをログしていたが、gRPCの成否はHTTPステータスが
    // 常に200のため見えなかった。LoggingTaskGrpcService(grpc/LoggingTaskGrpcService.scala)で
    // 型付きの業務ロジック層を直接ラップする方式に切り替え、実際のio.grpc.Status.Codeまで
    // ログできるようにした
    val grpcHandler = TaskServicePowerApiHandler.partial(
      new grpc.LoggingTaskGrpcService(new TaskGrpcServiceImpl(taskService, dispatcher), LoggerFactory.getLogger("grpc"))
    )

    // CONTRACT.mdセクション11・20.7: 外部公開API。backend.external-tasks-pagination-v2は
    // 5言語で共有する1つのflagのため、backend自身がfeature_flagsテーブルを直接ポーリングして評価する
    val flagRepo = new SlickFeatureFlagRepository(db)
    val flagPoller = new FeatureFlagPoller(flagRepo, "backend.external-tasks-pagination-v2", defaultValue = false)
    val externalRoutes = RequestLogging(LoggerFactory.getLogger("http.external"))(
      new ExternalTaskRoutes(taskService, dispatcher, cfg.externalApiClientId, flagPoller).routes
    )

    def parseAddr(addr: String, default: Int): (String, Int) = {
      val trimmed = addr.stripPrefix(":")
      if (trimmed.isEmpty) ("0.0.0.0", default)
      else trimmed.split(":").toList match {
        case host :: port :: Nil => (host, port.toInt)
        case portOnly :: Nil     => ("0.0.0.0", portOnly.toInt)
        case _                    => ("0.0.0.0", default)
      }
    }

    val (restHost, restPort) = parseAddr(cfg.httpAddr, 8095)
    val (grpcHost, grpcPort) = parseAddr(cfg.grpcAddr, 9095)
    val (externalHost, externalPort) = parseAddr(cfg.externalHttpAddr, 8100)

    Http().newServerAt(restHost, restPort).bind(restRoutes).onComplete {
      case Success(binding) => logger.info(s"REST v1(Pekko HTTP)起動: ${binding.localAddress}")
      case Failure(ex)      => logger.error("REST v1の起動に失敗", ex); system.terminate()
    }

    Http().newServerAt(externalHost, externalPort).bind(externalRoutes).onComplete {
      case Success(binding) => logger.info(s"外部公開API(Pekko HTTP)起動: ${binding.localAddress}")
      case Failure(ex)      => logger.error("外部公開APIの起動に失敗", ex); system.terminate()
    }

    // pekko-grpcはHTTP/2ハンドラ。TLS無し(h2c)での起動はコード側ではなく
    // application.conf の pekko.http.server.preview.enable-http2 = on で有効化する
    // (akka-grpc/pekko-grpc公式のサーバー起動手順と同じ。Goのgrpc.NewServerがinsecureで
    // 起動できるのに対し、PekkoではHTTP/2プレビュー機能を明示的に有効にする必要がある)
    Http().newServerAt(grpcHost, grpcPort).bind(grpcHandler).onComplete {
      case Success(binding) => logger.info(s"gRPC v2(pekko-grpc)起動: ${binding.localAddress}")
      case Failure(ex)      => logger.error("gRPC v2の起動に失敗", ex); system.terminate()
    }
  }
}
