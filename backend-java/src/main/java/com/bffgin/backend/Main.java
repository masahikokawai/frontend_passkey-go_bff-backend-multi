package com.bffgin.backend;

import com.bffgin.backend.auth.Dispatcher;
import com.bffgin.backend.auth.HmacVerifier;
import com.bffgin.backend.auth.JwksVerifier;
import com.bffgin.backend.auth.UserResolver;
import com.bffgin.backend.domain.TaskError;
import com.bffgin.backend.external.ExternalHandler;
import com.bffgin.backend.flags.FeatureFlagPoller;
import com.bffgin.backend.grpc.GrpcAuthInterceptor;
import com.bffgin.backend.grpc.TaskGrpcService;
import com.bffgin.backend.repository.TaskRepository;
import com.bffgin.backend.rest.RestErrorMapper;
import com.bffgin.backend.rest.TaskRestHandler;
import com.zaxxer.hikari.HikariConfig;
import com.zaxxer.hikari.HikariDataSource;
import io.grpc.Grpc;
import io.grpc.InsecureServerCredentials;
import io.grpc.ServerInterceptors;
import io.javalin.Javalin;
import org.eclipse.jetty.util.thread.QueuedThreadPool;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.concurrent.Executors;

public final class Main {

    /*
     * LOG_LEVEL(既定info、debug/info/warn/errorを想定、backend(Go)/bff/gateway/goと同じ環境変数名)。
     * slf4j-simpleはSimpleLoggerConfiguration初期化時(=プロセス内で最初にLoggerFactory.getLoggerが
     * 呼ばれた時点)にシステムプロパティorg.slf4j.simpleLogger.defaultLogLevelを読むため、
     * このクラスのstatic初期化子(下のlogフィールドの初期化より前に実行される)で
     * 他のどのクラスのLoggerよりも先に設定する必要がある
     */
    static {
        String level = System.getenv("LOG_LEVEL");
        System.setProperty("org.slf4j.simpleLogger.defaultLogLevel", (level == null || level.isEmpty()) ? "info" : level);
    }

    private static final Logger log = LoggerFactory.getLogger(Main.class);

    public static void main(String[] args) throws Exception {
        Config config = Config.fromEnv();
        log.info("backend-java starting: HTTP_ADDR=:{} GRPC_ADDR=:{} db={}:{}/{}",
                config.httpAddr(), config.grpcAddr(), config.dbHost(), config.dbPort(), config.dbSchema());

        HikariDataSource dataSource = buildDataSource(config);
        TaskRepository repository = new TaskRepository(dataSource);

        // 3issuerのJWT検証Dispatcher(backend-rust/backend-c/backend-cppと同じ構成)
        Dispatcher dispatcher = new Dispatcher()
                .register(Dispatcher.LOCAL_HMAC_ISSUER,
                        new HmacVerifier(config.localHmacSecret(), Dispatcher.LOCAL_HMAC_ISSUER, config.expectedAudience()))
                .register(Dispatcher.LOCAL_RSA_ISSUER,
                        new JwksVerifier(config.localRsaJwksUrl(), Dispatcher.LOCAL_RSA_ISSUER, config.expectedAudience()))
                .register(config.keycloakIssuer(),
                        new JwksVerifier(config.keycloakJwksUrl(), config.keycloakIssuer(), config.expectedAudience()));

        UserResolver userResolver = new UserResolver(dispatcher, repository);

        FeatureFlagPoller flagPoller = new FeatureFlagPoller(dataSource);
        flagPoller.start();

        startRestServer(config, repository, userResolver);
        startGrpcServer(config, repository, userResolver);
        startExternalServer(config, repository, dispatcher, flagPoller);

        log.info("backend-java listening: REST=:{} GRPC=:{} EXTERNAL=:{}",
                config.httpAddr(), config.grpcAddr(), config.externalHttpAddr());
    }

    private static HikariDataSource buildDataSource(Config config) {
        HikariConfig hikariConfig = new HikariConfig();
        hikariConfig.setJdbcUrl(config.jdbcUrl());
        hikariConfig.setUsername(config.dbUser());
        hikariConfig.setPassword(config.dbPassword());
        hikariConfig.setMaximumPoolSize(10);
        return new HikariDataSource(hikariConfig);
    }

    /**
     * Virtual Threads(JDK21+ Project Loom)でリクエストを処理する。
     * QueuedThreadPool自体はJettyのライフサイクル/バックプレッシャー管理のために残しつつ、
     * 実際の処理はvirtual-thread-per-taskのExecutorへ委譲する(Jetty 12以降がサポートする構成)。
     * これにより、JDBCのような普通のブロッキング呼び出しをそのまま書いても、
     * OSスレッドを専有せずに済む(README.md「アーキテクチャ選定」節参照)
     *
     * 【Kotlin実装との対比】この仕組みは「並行処理の安全性を自動化する」設計であり、
     * TaskRepositoryのJDBC呼び出し箇所には特別な記述は一切不要(通常のブロッキング呼び出しの
     * ままでよい)。これは、同じくJVM上で実装予定のKotlin(Ktor+コルーチン)が
     * 「並行処理の安全性を型システムと明示的なディスパッチャ選択(withContext(Dispatchers.IO))で
     * 保証する」設計を採るのと意図的に対照的である(backend-kotlin/README.md参照)。
     * JavaのJVMがI/Oブロックを検知して自動的にキャリアスレッドを解放するのに対し、
     * Kotlinでは呼び出し側が「これはブロッキングI/Oである」と自己申告する必要がある、という違い
     */
    private static void startRestServer(Config config, TaskRepository repository, UserResolver userResolver) {
        TaskRestHandler handler = new TaskRestHandler(repository, userResolver);

        QueuedThreadPool threadPool = new QueuedThreadPool();
        threadPool.setVirtualThreadsExecutor(Executors.newVirtualThreadPerTaskExecutor());

        Javalin app = Javalin.create(cfg -> cfg.jetty.threadPool = threadPool);

        // リクエスト単位のログ(method/path/実際のステータス/duration_ms)。gRPC側のTaskGrpcService.run()と
        // 同じkey=value形式。before/afterはJavalinの例外ハンドラより後に実行されるため、
        // TaskError発生時にRestErrorMapperが書き込んだ実際のステータスコードをctx.status()で正しく取得できる
        app.before(ctx -> ctx.attribute("startNanos", System.nanoTime()));
        app.after(ctx -> {
            Long startNanos = ctx.attribute("startNanos");
            long durationMs = startNanos == null ? 0 : (System.nanoTime() - startNanos) / 1_000_000;
            log.info("rest method={} path={} status={} duration_ms={}", ctx.method(), ctx.path(), ctx.status(), durationMs);
        });

        app.get("/internal/v1/tasks", handler.list);
        app.post("/internal/v1/tasks", handler.create);
        app.get("/internal/v1/tasks/{id}", handler.get);
        app.patch("/internal/v1/tasks/{id}", handler.update);
        app.delete("/internal/v1/tasks/{id}", handler.delete);

        app.exception(TaskError.class, (e, ctx) -> RestErrorMapper.write(ctx, e));
        app.exception(Exception.class, (e, ctx) -> {
            log.error("unhandled exception", e);
            RestErrorMapper.write(ctx, TaskError.internal(e.getMessage()));
        });

        int port = Integer.parseInt(config.httpAddr());
        app.start(port);
    }

    /**
     * 外部公開API(:8112配下)専用の、3つ目の独立したJavalinインスタンス。
     * 内部REST(:8111)とは別ポート・別認証モデル(Client Credentials Grantのみ)であるため、
     * 同じJavalinインスタンスにパスを増やすのではなく、独立したリスナーとして分離する
     * (CONTRACT.mdセクション11「ネットワーク公開」参照。内部REST/gRPCとは異なるネットワーク
     * スコープからの到達を想定した設計)
     */
    private static void startExternalServer(Config config, TaskRepository repository, Dispatcher dispatcher,
            FeatureFlagPoller flagPoller) {
        ExternalHandler handler = new ExternalHandler(repository, dispatcher, flagPoller, config.externalApiClientId());

        QueuedThreadPool threadPool = new QueuedThreadPool();
        threadPool.setVirtualThreadsExecutor(Executors.newVirtualThreadPerTaskExecutor());

        Javalin app = Javalin.create(cfg -> cfg.jetty.threadPool = threadPool);

        // 内部RESTと同じ形式のリクエスト単位ログ(external接頭辞で区別する)
        app.before(ctx -> ctx.attribute("startNanos", System.nanoTime()));
        app.after(ctx -> {
            Long startNanos = ctx.attribute("startNanos");
            long durationMs = startNanos == null ? 0 : (System.nanoTime() - startNanos) / 1_000_000;
            log.info("external method={} path={} status={} duration_ms={}", ctx.method(), ctx.path(), ctx.status(), durationMs);
        });

        app.get("/external/v1/tasks", handler.list);
        app.exception(TaskError.class, (e, ctx) -> RestErrorMapper.write(ctx, e));
        app.exception(Exception.class, (e, ctx) -> {
            log.error("unhandled exception (external)", e);
            RestErrorMapper.write(ctx, TaskError.internal(e.getMessage()));
        });

        app.start(Integer.parseInt(config.externalHttpAddr()));
    }

    private static void startGrpcServer(Config config, TaskRepository repository, UserResolver userResolver)
            throws java.io.IOException {
        TaskGrpcService service = new TaskGrpcService(repository, userResolver);
        int port = Integer.parseInt(config.grpcAddr());
        var server = Grpc.newServerBuilderForPort(port, InsecureServerCredentials.create())
                .addService(ServerInterceptors.intercept(service, new GrpcAuthInterceptor()))
                .build()
                .start();

        Runtime.getRuntime().addShutdownHook(new Thread(server::shutdown));
    }
}
