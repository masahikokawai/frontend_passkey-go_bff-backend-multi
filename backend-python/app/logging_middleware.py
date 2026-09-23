from __future__ import annotations

import logging
import time

from fastapi import FastAPI, Request

log = logging.getLogger(__name__)


def add_request_logging(app: FastAPI, prefix: str) -> None:
    """リクエスト単位のログをINFOで1行出す(backend-python本体のgRPCログ
    (`app/grpc_/service.py`の`grpc method=... status=... duration_ms=...`)と同じ
    key=value形式。prefixは"rest"(内部REST v1)/"external"(外部公開API)を渡す想定。

    【このプロジェクトで判明していたギャップ】backend-pythonはgRPCの`_logged`ラッパーで
    RPC単位のログを既に持っていたが、REST v1・外部公開APIには同種のリクエスト単位ログが
    無かった(README.mdの多言語比較テーブルの見直しで判明)。個々のハンドラへ手書きで
    埋め込むのではなく、Starlette/FastAPIの`@app.middleware("http")`として1箇所に
    実装することで、両アプリの全ルートに漏れなく適用されるようにしている
    """

    @app.middleware("http")
    async def _log_requests(request: Request, call_next):
        start = time.perf_counter()
        response = await call_next(request)
        duration_ms = int((time.perf_counter() - start) * 1000)
        log.info(
            "%s method=%s path=%s status=%d duration_ms=%d",
            prefix,
            request.method,
            request.url.path,
            response.status_code,
            duration_ms,
        )
        return response
