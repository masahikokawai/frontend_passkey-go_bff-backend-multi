from __future__ import annotations

import asyncio
import logging

import aiomysql

log = logging.getLogger("backend-python.flags")

POLL_INTERVAL_SECONDS = 10


class FeatureFlagPoller:
    """feature_flagsテーブルを10秒間隔で直接ポーリングする(bffのようなHTTPポーリングではない、
    backend-java/backend-kotlin/backend-c/backend-cppと同じ設計)。外部公開API
    (backend.external-tasks-pagination-v2)の判定に使う。

    aiomysqlはこのPython実装の他の全てのDBアクセスと同じ非同期ネイティブドライバであり、
    ポーリング専用の別プールを持つ必要すら無い(共有プールを使い回すだけでイベントループを
    ブロックしない)。キャッシュの更新はモジュールレベルの辞書を「置き換える」(mutateしない)
    ことで行う。CPythonのGILにより単一の代入操作は他のコルーチンから見て中断されないため、
    読み取り側がロックを取る必要は無い(この実装の全体テーマである「明示的な隔離が不要」の
    もう一つの実演)
    """

    def __init__(self, pool: aiomysql.Pool) -> None:
        self._pool = pool
        self._entries: dict[str, tuple[bool, str]] = {}
        self._task: asyncio.Task | None = None

    def start(self) -> None:
        self._task = asyncio.create_task(self._run())

    async def stop(self) -> None:
        if self._task is not None:
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass

    async def _run(self) -> None:
        while True:
            await self._poll_once()
            await asyncio.sleep(POLL_INTERVAL_SECONDS)

    async def _poll_once(self) -> None:
        try:
            async with self._pool.acquire() as conn:
                async with conn.cursor() as cur:
                    await cur.execute("SELECT flag_key, enabled, default_variation FROM feature_flags")
                    rows = await cur.fetchall()
            self._entries = {row[0]: (bool(row[1]), row[2]) for row in rows}
        except Exception as exc:  # noqa: BLE001 (ポーリング失敗はログのみ、既存キャッシュを保持する)
            log.warning("feature_flagsのポーリングに失敗しました: %s", exc)

    def variation(self, flag_key: str, default_value: str) -> str:
        """enabled=Falseの場合、または未知のflag_keyの場合はdefault_valueを返す"""
        entry = self._entries.get(flag_key)
        if entry is None or not entry[0]:
            return default_value
        return entry[1]
