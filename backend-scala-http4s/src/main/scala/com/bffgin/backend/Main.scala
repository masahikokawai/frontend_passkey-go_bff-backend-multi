package com.bffgin.backend

import cats.effect.{IO, IOApp, ExitCode}
import cats.syntax.all._
import com.comcast.ip4s._
import org.http4s.ember.server.EmberServerBuilder
import org.http4s.server.Router
import org.http4s.server.middleware.{Logger => RequestLogger}
import org.typelevel.log4cats.slf4j.Slf4jLogger
import com.bffgin.backend.auth.JwtAuth
import com.bffgin.backend.rest.TaskRoutes
import com.bffgin.backend.grpc.TaskGrpcServiceImpl
import com.bffgin.backend.external.ExternalTaskRoutes
// grpc-netty-shaded は io.grpc.netty パッケージ自体も io.grpc.netty.shaded.io.grpc.netty へ
// 再配置しているため、通常のgrpc-nettyとはimportパスが異なる点に注意
import io.grpc.netty.shaded.io.grpc.netty.NettyServerBuilder
import io.grpc.ServerInterceptors
import com.bffgin.backend.grpc.LoggingServerInterceptor
import task.v1.task.TaskServiceFs2Grpc

object Main extends IOApp {
  // Go実装(backend/cmd/server/main.go)が1プロセスでREST(Gin)とgRPCを両方起動しているのと同じ構成
  // (CONTRACT.mdセクション20.6)
  // DBコネクションプールとTaskServiceを共有する
  override def run(args: List[String]): IO[ExitCode] = {
    val cfg = Config.load()
    for {
      logger <- Slf4jLogger.create[IO]
      _      <- logger.info(s"backend-scala-http4s起動準備: http=${cfg.httpAddr} grpc=${cfg.grpcAddr}")
      exitCode <- Db.transactor(cfg).use { xa =>
        val repo    = new TaskRepo(xa)
        val service = new TaskService(repo)
        val auth    = new JwtAuth(cfg)

        // 【手動検証で発覚・修正】以前はこのbackend自身にリクエスト単位のログが1行も無く、
        // 起動時の3行(下記logger.info呼び出し)以外は何も出ていなかった
        // (Go実装のrequestLogger・Rails実装の標準ログと違い、backend.task-languageで
        // 実際にこの言語が処理したことをログだけでは確認できなかった)。
        // http4sの標準ミドルウェアLogger.httpAppでmethod/path/status/durationを
        // 出力するようにする。logHeaders/logBodyはいずれもfalseにし、
        // Authorizationヘッダやタスク名等の個人情報を含みうる本文をログに残さないようにする
        val restApp    = RequestLogger.httpApp(logHeaders = false, logBody = false, logAction = Some((msg: String) => logger.info(msg)))(
          Router("/" -> new TaskRoutes(service, repo, auth).routes).orNotFound
        )
        val grpcImpl    = new TaskGrpcServiceImpl(service, repo, auth)
        val httpPort    = Port.fromString(cfg.httpAddr.stripPrefix(":")).getOrElse(port"8094")
        val grpcPortNum = cfg.grpcAddr.stripPrefix(":").toInt
        val externalHttpPort = Port.fromString(cfg.externalHttpAddr.stripPrefix(":")).getOrElse(port"8099")

        val httpServer = EmberServerBuilder
          .default[IO]
          .withHost(host"0.0.0.0")
          .withPort(httpPort)
          .withHttpApp(restApp)
          .build

        // 【手動検証で発覚・修正、REST/外部APIのログ追加時に一旦見送った箇所】
        // ServerInterceptors.interceptでLoggingServerInterceptor(grpcパッケージ)をかませ、
        // gRPCもmethod/status/duration_msのログを出すようにする
        val grpcServer = TaskServiceFs2Grpc.bindServiceResource[IO](grpcImpl).flatMap { serviceDef =>
          val loggedServiceDef = ServerInterceptors.intercept(serviceDef, new LoggingServerInterceptor)
          cats.effect.Resource.make(IO.blocking {
            val server = NettyServerBuilder.forPort(grpcPortNum).addService(loggedServiceDef).build()
            server.start()
            server
          })(server => IO.blocking(server.shutdown()).void)
        }

        // CONTRACT.mdセクション11・20.7: BFFを経由しない外部公開API
        // 内部 REST/gRPC とは別ポート・別ミドルウェア(Client Credentials Grant)で待ち受ける
        val externalServer = for {
          extFlags <- ExternalFlags.resource(repo)
          externalApp = RequestLogger.httpApp(logHeaders = false, logBody = false, logAction = Some((msg: String) => logger.info(msg)))(
            Router(
              "/" -> new ExternalTaskRoutes(repo, auth, extFlags, cfg.externalApiClientId).routes
            ).orNotFound
          )
          srv <- EmberServerBuilder
            .default[IO]
            .withHost(host"0.0.0.0")
            .withPort(externalHttpPort)
            .withHttpApp(externalApp)
            .build
        } yield srv

        (httpServer, grpcServer, externalServer).tupled.use { _ =>
          logger.info(s"REST v1(http4s)起動: ${cfg.httpAddr}") *>
            logger.info(s"gRPC v2(fs2-grpc)起動: ${cfg.grpcAddr}") *>
            logger.info(s"外部公開API起動: ${cfg.externalHttpAddr}") *>
            IO.never
        }
      }
    } yield exitCode
  }.as(ExitCode.Success)
}
