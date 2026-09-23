package com.bffgin.backend

import com.bffgin.backend.auth.Dispatcher
import com.bffgin.backend.auth.HmacVerifier
import com.bffgin.backend.auth.JwksVerifier
import com.bffgin.backend.auth.UserResolver
import com.bffgin.backend.flags.FeatureFlagPoller
import com.bffgin.backend.grpc.GrpcAuthInterceptor
import com.bffgin.backend.grpc.TaskGrpcService
import com.bffgin.backend.repository.TaskRepository
import com.bffgin.backend.rest.externalRoutes
import com.bffgin.backend.rest.taskRoutes
import com.zaxxer.hikari.HikariConfig
import com.zaxxer.hikari.HikariDataSource
import io.grpc.Grpc
import io.grpc.InsecureServerCredentials
import io.grpc.ServerInterceptors
import io.ktor.server.application.ApplicationCallPipeline
import io.ktor.server.application.call
import io.ktor.server.cio.CIO
import io.ktor.server.engine.embeddedServer
import io.ktor.server.request.httpMethod
import io.ktor.server.request.path
import io.ktor.server.routing.routing
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import org.slf4j.LoggerFactory

private val log = LoggerFactory.getLogger("com.bffgin.backend.Main")
private val restLog = LoggerFactory.getLogger("com.bffgin.backend.rest")
private val externalLog = LoggerFactory.getLogger("com.bffgin.backend.external")

fun main() {
    val config = Config.fromEnv()
    log.info(
        "backend-kotlin starting: HTTP_ADDR=:{} GRPC_ADDR=:{} db={}:{}/{}",
        config.httpAddr, config.grpcAddr, config.dbHost, config.dbPort, config.dbSchema,
    )

    val dataSource = buildDataSource(config)
    val repository = TaskRepository(dataSource)

    // 3issuerのJWT検証Dispatcher(backend-java/backend-rust/backend-c/backend-cppと同じ構成)
    val dispatcher = Dispatcher()
        .register(
            Dispatcher.LOCAL_HMAC_ISSUER,
            HmacVerifier(config.localHmacSecret, Dispatcher.LOCAL_HMAC_ISSUER, config.expectedAudience),
        )
        .register(
            Dispatcher.LOCAL_RSA_ISSUER,
            JwksVerifier(config.localRsaJwksUrl, Dispatcher.LOCAL_RSA_ISSUER, config.expectedAudience),
        )
        .register(
            config.keycloakIssuer,
            JwksVerifier(config.keycloakJwksUrl, config.keycloakIssuer, config.expectedAudience),
        )

    val userResolver = UserResolver(dispatcher, repository)

    // feature_flagsテーブルの直接ポーリング(10秒間隔固定、backend-java/backend-rust/
    // backend-cpp/backend-cと同じ設計)。ポーラー自身のライフタイムは、いずれのHTTP/gRPC
    // サーバーのリクエストスコープにも縛られないため、専用のCoroutineScope(SupervisorJobで
    // 子の失敗が親や他の子に伝播しないようにする)を割り当てる
    val backgroundScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    val flagPoller = FeatureFlagPoller(dataSource)
    flagPoller.start(backgroundScope)

    startRestServer(config, repository, userResolver)
    val grpcServer = startGrpcServer(config, repository, userResolver)
    startExternalServer(config, repository, dispatcher, flagPoller)

    log.info(
        "backend-kotlin listening: REST=:{} GRPC=:{} EXTERNAL=:{}",
        config.httpAddr, config.grpcAddr, config.externalHttpAddr,
    )

    // 【実機検証で判明】Ktorのコルーチンネイティブなエンジン(CIO)は、kotlinx.coroutinesの
    // デフォルトディスパッチャ(デーモンスレッド)上で動くため、main()がここで単純に返ると
    // 他に非デーモンスレッドが無い限りJVMプロセスごと終了してしまう(backend-javaのJetty/
    // Virtual Threadsは非デーモンスレッドを保持するため、この明示的なブロックが無くても
    // プロセスが生き続けていた)。grpc-java公式サンプルと同じ`awaitTermination()`で
    // メインスレッドを明示的にブロックし、シグナル(SIGINT/SIGTERM)によるシャットダウンまで
    // プロセスを維持する
    grpcServer.awaitTermination()
}

private fun buildDataSource(config: Config): HikariDataSource {
    val hikariConfig = HikariConfig()
    hikariConfig.jdbcUrl = config.jdbcUrl
    hikariConfig.username = config.dbUser
    hikariConfig.password = config.dbPassword
    hikariConfig.maximumPoolSize = 10
    return HikariDataSource(hikariConfig)
}

/**
 * KtorのCIOエンジンはコルーチンネイティブな非同期I/Oでリクエストを処理する。
 * TaskRepositoryの各メソッドが内部でwithContext(Dispatchers.IO)を使ってJDBC呼び出しを
 * 明示的に隔離しているため、ここでは普通にsuspend funを呼ぶだけでよい
 * (TaskRepository.ktのクラスコメント・README.md「アーキテクチャ選定」節参照)。
 *
 * 【構造化並行性】Ktorは各リクエストを、そのリクエストのライフサイクルに紐づく
 * コルーチンスコープ上で処理する。クライアントが接続を切断すればそのスコープがキャンセルされ、
 * 配下で起動された子コルーチンにもキャンセルが伝播する。これはbackend-java(Main.java参照)の
 * スレッド割り込みに基づく、より粗い取り消しモデルとは対照的な、Kotlinコルーチン特有の
 * 精密な取り消し伝播モデルである
 */
private fun startRestServer(config: Config, repository: TaskRepository, userResolver: UserResolver) {
    val port = config.httpAddr.toInt()
    embeddedServer(CIO, port = port) {
        installRequestLogging(restLog, "rest")
        routing {
            taskRoutes(repository, userResolver)
        }
    }.start(wait = false)
}

/** 外部公開API(:8114配下、CONTRACT.mdセクション11)。内部REST v1とは別の独立したKtorインスタンス */
private fun startExternalServer(
    config: Config,
    repository: TaskRepository,
    dispatcher: Dispatcher,
    flagPoller: FeatureFlagPoller,
) {
    val port = config.externalHttpAddr.toInt()
    embeddedServer(CIO, port = port) {
        installRequestLogging(externalLog, "external")
        routing {
            externalRoutes(repository, dispatcher, flagPoller, config.externalApiClientId)
        }
    }.start(wait = false)
}

/**
 * リクエスト単位のログ(method/path/status/duration_ms)を1行のINFOログとして出す
 * (grpc method=...(TaskGrpcService.kt)と同じキー=値形式)。CallLoggingプラグインではなく
 * `intercept(ApplicationCallPipeline.Monitoring)`を明示的に書く理由は、Phoenixではなく
 * Plug相当のKtorを選んだのと同じ「フレームワークの魔法に頼らず何が起きているか見せる」方針
 * (README.md「アーキテクチャ選定」参照)。Monitoringフェーズはルーティング(Callフェーズ)全体を
 * 包むため、`proceed()`の前後でリクエストの開始/終了をちょうど1回ずつ観測できる
 */
private fun io.ktor.server.application.Application.installRequestLogging(
    logger: org.slf4j.Logger,
    label: String,
) {
    intercept(ApplicationCallPipeline.Monitoring) {
        val start = System.currentTimeMillis()
        proceed()
        val durationMs = System.currentTimeMillis() - start
        logger.info(
            "{} method={} path={} status={} duration_ms={}",
            label,
            call.request.httpMethod.value,
            call.request.path(),
            call.response.status()?.value,
            durationMs,
        )
    }
}

private fun startGrpcServer(
    config: Config,
    repository: TaskRepository,
    userResolver: UserResolver,
): io.grpc.Server {
    val service = TaskGrpcService(repository, userResolver)
    val port = config.grpcAddr.toInt()
    val server = Grpc.newServerBuilderForPort(port, InsecureServerCredentials.create())
        .addService(ServerInterceptors.intercept(service, GrpcAuthInterceptor()))
        .build()
        .start()

    Runtime.getRuntime().addShutdownHook(Thread { server.shutdown() })
    return server
}
