from __future__ import annotations

import pytest

from app.flags.feature_flag_poller import FeatureFlagPoller
from tests.integration.db_fixture import build_test_pool

pytestmark = pytest.mark.integration

FLAG_KEY_PREFIX = "backend-python-test-flag"


@pytest.fixture
async def db_pool():
    pool = await build_test_pool()
    yield pool
    pool.close()
    await pool.wait_closed()


@pytest.fixture
async def test_flag_key(db_pool):
    """既存の共有flag行(backend.external-tasks-pagination-v2等)には一切触れない、
    テスト専用の使い捨てflag_keyを挿入・削除する
    """
    import time

    key = f"{FLAG_KEY_PREFIX}-{int(time.time() * 1_000_000)}"
    yield key
    async with db_pool.acquire() as conn:
        async with conn.cursor() as cur:
            await cur.execute("DELETE FROM feature_flags WHERE flag_key = %s", (key,))


async def _insert_flag(pool, key: str, enabled: bool, default_variation: str) -> None:
    async with pool.acquire() as conn:
        async with conn.cursor() as cur:
            await cur.execute(
                "INSERT INTO feature_flags (flag_key, description, default_variation, enabled, created_at, updated_at) "
                "VALUES (%s, %s, %s, %s, NOW(), NOW())",
                (key, "backend-python integration test", default_variation, enabled),
            )


async def test_variation_returns_default_variation_when_enabled(db_pool, test_flag_key):
    await _insert_flag(db_pool, test_flag_key, True, "on")
    poller = FeatureFlagPoller(db_pool)
    await poller._poll_once()
    assert poller.variation(test_flag_key, "off") == "on"


async def test_variation_returns_fallback_when_disabled(db_pool, test_flag_key):
    await _insert_flag(db_pool, test_flag_key, False, "on")
    poller = FeatureFlagPoller(db_pool)
    await poller._poll_once()
    assert poller.variation(test_flag_key, "off") == "off"


async def test_variation_returns_fallback_when_not_found(db_pool, test_flag_key):
    poller = FeatureFlagPoller(db_pool)
    await poller._poll_once()
    assert poller.variation(test_flag_key, "off") == "off"


async def test_start_stop_poll_loop_runs_without_error(db_pool, test_flag_key):
    """start()/stop()によるバックグラウンドタスクのライフサイクル自体が正しく機能することを確認する
    (即時ポーリングを待たず、ループ自体がキャンセル可能であることの実演)
    """
    await _insert_flag(db_pool, test_flag_key, True, "on")
    poller = FeatureFlagPoller(db_pool)
    poller.start()
    try:
        import asyncio

        await asyncio.sleep(0.2)
        await poller._poll_once()
        assert poller.variation(test_flag_key, "off") == "on"
    finally:
        await poller.stop()
