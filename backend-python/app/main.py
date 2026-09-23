from __future__ import annotations

import asyncio
import logging

import aiomysql
import grpc
import uvicorn
from fastapi import FastAPI
from pymysql.constants import CLIENT

from app.auth.dispatcher import LOCAL_HMAC_ISSUER, LOCAL_RSA_ISSUER, Dispatcher
from app.auth.hmac_verifier import HmacVerifier
from app.auth.jwks_verifier import JwksVerifier
from app.auth.user_resolver import UserResolver
from app import generated_path  # noqa: F401  side effect: adds app/generated to sys.path
from app.config import Config
from app.external.routes import create_external_router
from app.flags.feature_flag_poller import FeatureFlagPoller
from app.grpc_.service import TaskGrpcService
from app.logging_middleware import add_request_logging
from app.repository.task_repository import TaskRepository
from app.rest.routes import create_router, register_error_handler
from task.v1 import task_pb2_grpc

log = logging.getLogger("backend-python")

_LOG_LEVELS = {
    "debug": logging.DEBUG,
    "info": logging.INFO,
    "warn": logging.WARNING,
    "error": logging.ERROR,
}


def configure_logging(log_level: str) -> None:
    """backend(Go)/bff/gateway(Go)と同じLOG_LEVEL規約("debug"/"info"/"warn"/"error"、
    既定"info")。root loggerにのみ`basicConfig`でハンドラ・レベルを設定し、他モジュールは
    `logging.getLogger(__name__)`で子loggerを取るだけにする(このプロジェクトの既存の
    gRPCログ(`app/grpc_/service.py`)もこの前提で書かれている)。

    【Pythonのloggingの既知の落とし穴】`logging.basicConfig()`はrootロガーに既に
    ハンドラが付いていると何もしない(2回目以降の呼び出しは無視される)。このため、
    この関数は`main()`の先頭で環境変数を読み終えた直後、他の何よりも先に**1回だけ**
    呼ぶ必要がある(uvicorn.Config等が先に何らかのロガーを触ってからだと、期待した
    レベル・フォーマットが反映されない場合がある)
    """
    level = _LOG_LEVELS.get(log_level.lower(), logging.INFO)
    logging.basicConfig(level=level, format="%(asctime)s %(levelname)s %(name)s %(message)s")


async def build_pool(config: Config) -> aiomysql.Pool:
    return await aiomysql.create_pool(
        host=config.db_host,
        port=config.db_port,
        user=config.db_user,
        password=config.db_password,
        db=config.db_schema,
        minsize=1,
        maxsize=10,
        autocommit=True,
        # 【実機検証で発見した実バグ】CLIENT_FOUND_ROWSを指定しない場合、MySQLはUPDATE文の
        # rowcountを「WHERE句にマッチした行数」ではなく「実際に値が変化した行数」として返す。
        # DATETIME列は秒精度しか持たないため、同じ秒内に作成・更新すると(かつ他のSET対象列の
        # 値も偶然全て同じだと)updated_atを含め1列も値が変わらず、WHERE句は行にマッチしている
        # のにrowcount=0になりうる。TaskRepository.update()はrowcount==0を
        # 「対象行が見つからない」の判定に使っているため、これを立てないと正当な更新が
        # 誤って404として扱われる(結合テストで実際に再現・修正した既知バグ、README.md参照)
        client_flag=CLIENT.FOUND_ROWS,
    )


def build_dispatcher(config: Config) -> Dispatcher:
    """3issuerのJWT検証Dispatcher(backend-java/backend-kotlin/backend-rust/backend-c/backend-cppと
    同じ構成)
    """
    return (
        Dispatcher()
        .register(LOCAL_HMAC_ISSUER, HmacVerifier(config.local_hmac_secret, LOCAL_HMAC_ISSUER, config.expected_audience))
        .register(LOCAL_RSA_ISSUER, JwksVerifier(config.local_rsa_jwks_url, LOCAL_RSA_ISSUER, config.expected_audience))
        .register(
            config.keycloak_issuer,
            JwksVerifier(config.keycloak_jwks_url, config.keycloak_issuer, config.expected_audience),
        )
    )


def build_fastapi_app(repository: TaskRepository, user_resolver: UserResolver) -> FastAPI:
    app = FastAPI()
    add_request_logging(app, "rest")
    app.include_router(create_router(repository, user_resolver))
    register_error_handler(app)
    return app


def build_external_app(
    repository: TaskRepository, dispatcher: Dispatcher, flags: FeatureFlagPoller, external_api_client_id: str
) -> FastAPI:
    app = FastAPI()
    add_request_logging(app, "external")
    app.include_router(create_external_router(repository, dispatcher, flags, external_api_client_id))
    return app


async def run_rest_server(config: Config, app: FastAPI) -> None:
    uvicorn_config = uvicorn.Config(app, host="0.0.0.0", port=int(config.http_addr), log_level="info")
    server = uvicorn.Server(uvicorn_config)
    await server.serve()


async def run_external_server(config: Config, app: FastAPI) -> None:
    uvicorn_config = uvicorn.Config(app, host="0.0.0.0", port=int(config.external_http_addr), log_level="info")
    server = uvicorn.Server(uvicorn_config)
    await server.serve()


async def run_grpc_server(config: Config, repository: TaskRepository, user_resolver: UserResolver) -> None:
    server = grpc.aio.server()
    service = TaskGrpcService(repository, user_resolver)
    task_pb2_grpc.add_TaskServiceServicer_to_server(service, server)
    server.add_insecure_port(f"0.0.0.0:{config.grpc_addr}")
    await server.start()
    log.info("backend-python gRPC listening: GRPC=:%s", config.grpc_addr)
    await server.wait_for_termination()


async def main() -> None:
    config = Config.from_env()
    configure_logging(config.log_level)
    log.info(
        "backend-python starting: HTTP_ADDR=:%s GRPC_ADDR=:%s db=%s:%s/%s",
        config.http_addr, config.grpc_addr, config.db_host, config.db_port, config.db_schema,
    )
    log.debug("LOG_LEVEL=%s", config.log_level)

    pool = await build_pool(config)
    repository = TaskRepository(pool)
    dispatcher = build_dispatcher(config)
    user_resolver = UserResolver(dispatcher, repository)

    flag_poller = FeatureFlagPoller(pool)
    flag_poller.start()

    fastapi_app = build_fastapi_app(repository, user_resolver)
    external_app = build_external_app(repository, dispatcher, flag_poller, config.external_api_client_id)

    log.info(
        "backend-python listening: REST=:%s GRPC=:%s EXTERNAL=:%s",
        config.http_addr, config.grpc_addr, config.external_http_addr,
    )

    try:
        await asyncio.gather(
            run_rest_server(config, fastapi_app),
            run_grpc_server(config, repository, user_resolver),
            run_external_server(config, external_app),
        )
    finally:
        await flag_poller.stop()
        pool.close()
        await pool.wait_closed()


if __name__ == "__main__":
    asyncio.run(main())
