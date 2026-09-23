from __future__ import annotations

import httpx
import pytest

from app.auth.dispatcher import LOCAL_HMAC_ISSUER, Dispatcher
from app.auth.hmac_verifier import HmacVerifier
from app.external.routes import PAGINATION_V2_FLAG_KEY, create_external_router
from app.flags.feature_flag_poller import FeatureFlagPoller
from app.repository.task_repository import TaskRepository
from fastapi import FastAPI
from tests.integration.db_fixture import DbTestFixture, build_test_pool
from tests.unit.support.test_token_helper import make_hmac_token

pytestmark = pytest.mark.integration

HMAC_SECRET = "test-secret-at-least-32-bytes-long!!"
EXTERNAL_CLIENT_ID = "external-api-client"


@pytest.fixture
async def db_pool():
    pool = await build_test_pool()
    yield pool
    pool.close()
    await pool.wait_closed()


@pytest.fixture
def fixture(db_pool):
    return DbTestFixture(db_pool)


@pytest.fixture
def repository(db_pool) -> TaskRepository:
    return TaskRepository(db_pool)


@pytest.fixture
def dispatcher() -> Dispatcher:
    """backend.external-tasks-pagination-v2以外の目的では実Keycloakを起動しない、という
    このプロジェクトの既存の精度(backend-rust/backend-cppのJWTユニットテストと同じ)に合わせ、
    ローカルHMACのみを登録する。ただしローカルHMACは外部公開APIでは常に拒否される対象、
    という点自体がテストの主題になる
    """
    return Dispatcher().register(LOCAL_HMAC_ISSUER, HmacVerifier(HMAC_SECRET, LOCAL_HMAC_ISSUER, "backend"))


class _FakeKeycloakVerifier:
    """実Keycloakを起動せずにazpチェックを結合テストするための、テスト専用の固定Claims検証器。
    署名検証自体は tests/unit/test_external_auth.py が実RSA鍵+モックJWKSサーバーで別途検証済みのため、
    ここでは「Keycloak発行相当のissuerとして扱われた場合の、azp/オフセット/カーソルの挙動」に絞る
    """

    def __init__(self, azp: str) -> None:
        self._azp = azp

    async def verify(self, token: str):
        from app.auth.claims import Claims

        return Claims(sub="user-does-not-matter", iss="fake-keycloak-issuer", azp=self._azp)


@pytest.fixture
def dispatcher_with_fake_keycloak() -> Dispatcher:
    d = Dispatcher().register(LOCAL_HMAC_ISSUER, HmacVerifier(HMAC_SECRET, LOCAL_HMAC_ISSUER, "backend"))
    d.register("fake-keycloak-issuer", _FakeKeycloakVerifier(EXTERNAL_CLIENT_ID))
    return d


def _build_app(repository: TaskRepository, dispatcher: Dispatcher, flags: FeatureFlagPoller) -> FastAPI:
    app = FastAPI()
    app.include_router(create_external_router(repository, dispatcher, flags, EXTERNAL_CLIENT_ID))
    return app


@pytest.fixture
def flags(db_pool) -> FeatureFlagPoller:
    return FeatureFlagPoller(db_pool)


@pytest.fixture
async def user_id(fixture: DbTestFixture):
    suffix = fixture.unique_suffix()
    uid = await fixture.create_user(suffix)
    yield uid
    await fixture.cleanup_user(uid)


def _fake_keycloak_token() -> str:
    """署名検証はdispatcher_with_fake_keycloakの_FakeKeycloakVerifierが固定で成功させるため、
    トークンの中身自体はダミーでよいが、Dispatcher.verify()は署名検証の前にpayloadを
    base64url decode+json.loadsして`iss`だけを覗く(2段構造)ため、その前段が通る程度の
    最小限のJWTの形は保っておく必要がある
    """
    import base64
    import json

    header_b64 = base64.urlsafe_b64encode(json.dumps({"alg": "RS256"}).encode()).rstrip(b"=").decode()
    payload_b64 = (
        base64.urlsafe_b64encode(json.dumps({"iss": "fake-keycloak-issuer"}).encode()).rstrip(b"=").decode()
    )
    return f"{header_b64}.{payload_b64}.fake-signature"


async def test_rejects_missing_authorization(repository, dispatcher, flags):
    app = _build_app(repository, dispatcher, flags)
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get("/external/v1/tasks", params={"user_id": "1"})
    assert resp.status_code == 401
    assert resp.json() == {"error": "unauthenticated"}


async def test_rejects_local_hmac_token_even_with_correct_azp(repository, dispatcher, flags, user_id):
    app = _build_app(repository, dispatcher, flags)
    token = make_hmac_token(HMAC_SECRET, LOCAL_HMAC_ISSUER, "backend", str(user_id), 3600, azp=EXTERNAL_CLIENT_ID)
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get(
            "/external/v1/tasks", params={"user_id": user_id}, headers={"Authorization": f"Bearer {token}"}
        )
    assert resp.status_code == 401


async def test_rejects_wrong_azp(repository, flags, user_id):
    dispatcher = Dispatcher()
    dispatcher.register("fake-keycloak-issuer", _FakeKeycloakVerifier("some-other-client"))
    app = _build_app(repository, dispatcher, flags)
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get(
            "/external/v1/tasks", params={"user_id": user_id}, headers={"Authorization": f"Bearer {_fake_keycloak_token()}"}
        )
    assert resp.status_code == 401


async def test_rejects_missing_user_id(repository, dispatcher_with_fake_keycloak, flags):
    app = _build_app(repository, dispatcher_with_fake_keycloak, flags)
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get(
            "/external/v1/tasks", headers={"Authorization": f"Bearer {_fake_keycloak_token()}"}
        )
    assert resp.status_code == 400
    assert resp.json() == {"error": "user_id_required"}


async def test_rejects_invalid_user_id(repository, dispatcher_with_fake_keycloak, flags):
    app = _build_app(repository, dispatcher_with_fake_keycloak, flags)
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        resp = await client.get(
            "/external/v1/tasks",
            params={"user_id": "not-a-number"},
            headers={"Authorization": f"Bearer {_fake_keycloak_token()}"},
        )
    assert resp.status_code == 400
    assert resp.json() == {"error": "invalid_user_id"}


async def test_offset_pagination_across_pages(repository, dispatcher_with_fake_keycloak, flags, user_id):
    """flag OFF(既定)なのでoffsetページングになる"""
    from datetime import date
    from app.domain.models import TaskInput, TaskStatus

    for i in range(3):
        await repository.create(
            user_id, TaskInput(f"ext-offset-{i}", None, "waiting", date(2099, 1, 1), []), TaskStatus.WAITING
        )

    app = _build_app(repository, dispatcher_with_fake_keycloak, flags)
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        resp1 = await client.get(
            "/external/v1/tasks",
            params={"user_id": user_id, "page": 1, "page_size": 2},
            headers={"Authorization": f"Bearer {_fake_keycloak_token()}"},
        )
        body1 = resp1.json()
        assert resp1.status_code == 200
        assert body1["page"] == 1
        assert body1["page_size"] == 2
        assert body1["total"] == 3
        assert len(body1["tasks"]) == 2
        assert "user_id" not in body1["tasks"][0]

        resp2 = await client.get(
            "/external/v1/tasks",
            params={"user_id": user_id, "page": 2, "page_size": 2},
            headers={"Authorization": f"Bearer {_fake_keycloak_token()}"},
        )
        body2 = resp2.json()
        assert len(body2["tasks"]) == 1


async def test_cursor_pagination_chains_to_null(repository, dispatcher_with_fake_keycloak, flags, user_id, fixture):
    """backend.external-tasks-pagination-v2をON→確認→OFFに復元する(全言語で共有するFeature Flagの
    ため、テスト後に必ず元の状態へ戻す)
    """
    from datetime import date
    from app.domain.models import TaskInput, TaskStatus

    ids = []
    for i in range(2):
        tid = await repository.create(
            user_id, TaskInput(f"ext-cursor-{i}", None, "waiting", date(2099, 1, 1), []), TaskStatus.WAITING
        )
        ids.append(tid)

    async with fixture.pool.acquire() as conn:
        async with conn.cursor() as cur:
            await cur.execute(
                "UPDATE feature_flags SET enabled=1, default_variation='on' "
                "WHERE flag_key = %s",
                (PAGINATION_V2_FLAG_KEY,),
            )
    try:
        await flags._poll_once()  # ポーリング間隔(10秒)を待たずに即時反映させる(テスト専用)
        app = _build_app(repository, dispatcher_with_fake_keycloak, flags)
        transport = httpx.ASGITransport(app=app)
        async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
            resp1 = await client.get(
                "/external/v1/tasks",
                params={"user_id": user_id, "limit": 1},
                headers={"Authorization": f"Bearer {_fake_keycloak_token()}"},
            )
            body1 = resp1.json()
            assert resp1.status_code == 200
            assert len(body1["tasks"]) == 1
            assert body1["next_cursor"] == str(ids[0])

            resp2 = await client.get(
                "/external/v1/tasks",
                params={"user_id": user_id, "cursor": body1["next_cursor"], "limit": 1},
                headers={"Authorization": f"Bearer {_fake_keycloak_token()}"},
            )
            body2 = resp2.json()
            assert len(body2["tasks"]) == 1
            assert body2["next_cursor"] == str(ids[1])

            resp3 = await client.get(
                "/external/v1/tasks",
                params={"user_id": user_id, "cursor": body2["next_cursor"], "limit": 1},
                headers={"Authorization": f"Bearer {_fake_keycloak_token()}"},
            )
            body3 = resp3.json()
            assert body3["tasks"] == []
            assert body3["next_cursor"] is None
    finally:
        async with fixture.pool.acquire() as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "UPDATE feature_flags SET enabled=0, default_variation='off' "
                    "WHERE flag_key = %s",
                    (PAGINATION_V2_FLAG_KEY,),
                )
