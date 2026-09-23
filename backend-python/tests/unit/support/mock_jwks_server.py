from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from tests.unit.support.test_token_helper import rsa_exponent_b64url, rsa_modulus_b64url


class MockJwksServer:
    """テスト専用の自プロセス内蔵JWKSサーバー(backend-java/backend-kotlin/backend-c/backend-cppの
    モックJWKSサーバーと同じ役割)。実際にKeycloak/bffを起動せずにJwksVerifierのRS256検証・
    kidキャッシュ・未知kid時の再取得ロジックを検証できる。テスト専用の道具として標準ライブラリの
    http.serverを使う(本番のREST実装はFastAPI/uvicornだが、これはあくまでテスト用の使い捨て
    モックであり、「本番はFastAPI、Verifierが叩く先はただのHTTPサーバーであれば何でもよい」
    という点を示す)
    """

    def __init__(self) -> None:
        self._keys: list[tuple[str, object]] = []
        server = self

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:  # noqa: N802 (http.server API name)
                body = json.dumps(server._build_jwks()).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, format: str, *args) -> None:  # noqa: A002 (silence test server logs)
                pass

        self._httpd = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self._thread = threading.Thread(target=self._httpd.serve_forever, daemon=True)
        self._thread.start()

    def add_key(self, kid: str, public_key) -> None:
        self._keys.append((kid, public_key))

    def jwks_url(self) -> str:
        port = self._httpd.server_address[1]
        return f"http://127.0.0.1:{port}/jwks"

    def _build_jwks(self) -> dict:
        keys = []
        for kid, public_key in self._keys:
            numbers = public_key.public_numbers()
            keys.append(
                {
                    "kty": "RSA",
                    "kid": kid,
                    "use": "sig",
                    "n": rsa_modulus_b64url(numbers),
                    "e": rsa_exponent_b64url(numbers),
                }
            )
        return {"keys": keys}

    def close(self) -> None:
        self._httpd.shutdown()
        self._httpd.server_close()

    def __enter__(self) -> "MockJwksServer":
        return self

    def __exit__(self, *_exc) -> None:
        self.close()
