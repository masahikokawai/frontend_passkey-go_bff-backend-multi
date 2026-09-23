from __future__ import annotations

import time
import uuid

import aiomysql
from pymysql.constants import CLIENT

from app.config import Config


class DbTestFixture:
    """実DB(docker-compose上のMySQL)結合テスト共通のfixture。
    テストごとに一意なemail/keycloak_subでユーザー・ラベルを作り、共有の開発用DBを汚さない
    (backend-java/backend-kotlin/backend-cpp/backend-cのDbTestFixture/db_fixtureと同じ設計。
    過去にbackend-rustで固定fixture行の共有が原因のテスト間干渉が見つかった経緯があるため、
    必ずテストごとに一意な行を作る)
    """

    def __init__(self, pool: aiomysql.Pool) -> None:
        self.pool = pool

    @staticmethod
    def unique_suffix() -> str:
        return f"{int(time.time() * 1_000_000)}-{uuid.uuid4().hex[:8]}"

    async def create_user(self, unique_suffix: str) -> int:
        async with self.pool.acquire() as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "INSERT INTO users (email, name, role, created_at, updated_at) VALUES (%s, %s, 1, NOW(), NOW())",
                    (f"backend-python-test-{unique_suffix}@example.com", f"backend-python-test-{unique_suffix}"),
                )
                return cur.lastrowid

    async def create_user_with_keycloak_sub(self, unique_suffix: str, keycloak_sub: str) -> int:
        user_id = await self.create_user(unique_suffix)
        async with self.pool.acquire() as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at) "
                    "VALUES (%s, %s, NOW(), NOW())",
                    (user_id, keycloak_sub),
                )
        return user_id

    async def create_label(self, unique_suffix: str) -> int:
        async with self.pool.acquire() as conn:
            async with conn.cursor() as cur:
                await cur.execute(
                    "INSERT INTO labels (name, created_at, updated_at) VALUES (%s, NOW(), NOW())",
                    (f"backend-python-test-label-{unique_suffix}",),
                )
                return cur.lastrowid

    async def cleanup_user(self, user_id: int) -> None:
        """ベストエフォートの後始末(backend-java/backend-kotlin/backend-c/backend-cppと同じ方針)"""
        try:
            async with self.pool.acquire() as conn:
                async with conn.cursor() as cur:
                    await cur.execute(
                        "DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE user_id = %s)",
                        (user_id,),
                    )
                    await cur.execute("DELETE FROM tasks WHERE user_id = %s", (user_id,))
                    await cur.execute("DELETE FROM user_keycloaks WHERE user_id = %s", (user_id,))
                    await cur.execute("DELETE FROM users WHERE id = %s", (user_id,))
        except Exception:
            pass

    async def cleanup_label(self, label_id: int) -> None:
        try:
            async with self.pool.acquire() as conn:
                async with conn.cursor() as cur:
                    await cur.execute("DELETE FROM labels WHERE id = %s", (label_id,))
        except Exception:
            pass

    async def count_task_labels(self, task_id: int) -> int:
        async with self.pool.acquire() as conn:
            async with conn.cursor() as cur:
                await cur.execute("SELECT COUNT(*) FROM task_labels WHERE task_id = %s", (task_id,))
                row = await cur.fetchone()
                return row[0]


async def build_test_pool() -> aiomysql.Pool:
    config = Config.from_env()
    return await aiomysql.create_pool(
        host=config.db_host,
        port=config.db_port,
        user=config.db_user,
        password=config.db_password,
        db=config.db_schema,
        minsize=1,
        maxsize=5,
        autocommit=True,
        # app/main.pyのbuild_pool()と同じ理由(UPDATE文のrowcountを「マッチ行数」にするため)
        client_flag=CLIENT.FOUND_ROWS,
    )
