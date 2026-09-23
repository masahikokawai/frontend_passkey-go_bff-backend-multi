from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone

import aiomysql

from app.domain.models import Label, Task, TaskInput, TaskStatus, User


@dataclass
class OffsetPage:
    tasks: list[Task]
    total: int


class TaskRepository:
    """生SQL + aiomysqlのみ(ORM禁止方針、他言語と統一)。コネクションプールはaiomysql.Poolが担う。

    【このPython実装で最も学習価値の高い設計判断、backend-java/Main.javaのstartRestServer()・
    backend-kotlin/TaskRepository.ktのクラスコメントと対になる、3言語目の比較】
    backend-java(Virtual Threads)は「並行処理の安全性を自動化する」設計(JVMがI/Oブロックを
    自動検知)、backend-kotlin(Dispatchers.IO)は「並行処理の安全性を型システムと明示的な
    ディスパッチャ選択で保証する」設計(呼び出し側が自己申告してディスパッチャを切り替える)
    だった。このPython実装は第3の解決策を示す: `aiomysql`は最初から非同期ネイティブな
    ドライバであり、`await cursor.execute(...)`は実際のソケットI/O待ちで自然にイベントループへ
    制御を返す。JDBCには「本質的に非同期なDB接続」という概念自体が存在せず、だからこそ
    Kotlinは明示的な隔離(withContext(Dispatchers.IO))が必要だったが、Pythonのasyncio
    エコシステムには`aiomysql`/`asyncmy`のようなドライバレベルで真に非同期なMySQLクライアントが
    存在するため、このクラスのどのメソッドにも「これはブロッキングI/Oである」と自己申告する
    ためのコードは一切登場しない(README.md「アーキテクチャ選定」節参照)
    """

    def __init__(self, pool: aiomysql.Pool) -> None:
        self._pool = pool

    async def list_offset(self, user_id: int, limit: int, offset: int) -> OffsetPage:
        """v1(REST)向け: offsetページング"""
        async with self._pool.acquire() as conn:
            async with conn.cursor(aiomysql.DictCursor) as cur:
                await cur.execute("SELECT COUNT(*) AS total FROM tasks WHERE user_id = %s", (user_id,))
                total_row = await cur.fetchone()
                total = total_row["total"]

                await cur.execute(
                    "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at "
                    "FROM tasks WHERE user_id = %s ORDER BY created_at DESC, id DESC LIMIT %s OFFSET %s",
                    (user_id, limit, offset),
                )
                rows = await cur.fetchall()
                tasks = [self._row_to_task(row) for row in rows]
            tasks = await self._attach_labels(conn, tasks)
        return OffsetPage(tasks=tasks, total=total)

    async def list_cursor(self, user_id: int, after_id: int, limit: int) -> list[Task]:
        """v2(gRPC)向け: id昇順のkeyset(cursor)ページング。
        cursorの実体はuint64(直前ページ最後のtask.id、0=先頭)、合成キーは使わない(CONTRACT.mdセクション5)
        """
        async with self._pool.acquire() as conn:
            async with conn.cursor(aiomysql.DictCursor) as cur:
                if after_id > 0:
                    await cur.execute(
                        "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at "
                        "FROM tasks WHERE user_id = %s AND id > %s ORDER BY id ASC LIMIT %s",
                        (user_id, after_id, limit),
                    )
                else:
                    await cur.execute(
                        "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at "
                        "FROM tasks WHERE user_id = %s ORDER BY id ASC LIMIT %s",
                        (user_id, limit),
                    )
                rows = await cur.fetchall()
                tasks = [self._row_to_task(row) for row in rows]
            return await self._attach_labels(conn, tasks)

    async def find_by_id(self, task_id: int, user_id: int) -> Task | None:
        """他ユーザーのtaskは見えない(所有権分離)"""
        async with self._pool.acquire() as conn:
            return await self._find_by_id_on_connection(conn, task_id, user_id)

    async def _find_by_id_on_connection(self, conn, task_id: int, user_id: int) -> Task | None:
        async with conn.cursor(aiomysql.DictCursor) as cur:
            await cur.execute(
                "SELECT id, name, description, status, finished_on, user_id, created_at, updated_at "
                "FROM tasks WHERE id = %s AND user_id = %s",
                (task_id, user_id),
            )
            row = await cur.fetchone()
            if row is None:
                return None
            task = self._row_to_task(row)
        tasks = await self._attach_labels(conn, [task])
        return tasks[0]

    async def create(self, user_id: int, input_: TaskInput, status: TaskStatus) -> int:
        async with self._pool.acquire() as conn:
            await conn.begin()
            try:
                now = datetime.now(timezone.utc).replace(tzinfo=None)
                async with conn.cursor() as cur:
                    await cur.execute(
                        "INSERT INTO tasks (name, description, status, finished_on, user_id, created_at, updated_at) "
                        "VALUES (%s, %s, %s, %s, %s, %s, %s)",
                        (input_.name, input_.description, status.db_value, input_.finished_on, user_id, now, now),
                    )
                    task_id = cur.lastrowid
                await self._replace_labels(conn, task_id, input_.label_ids, now)
                await conn.commit()
                return task_id
            except Exception:
                await conn.rollback()
                raise

    async def update(self, task_id: int, user_id: int, input_: TaskInput, status: TaskStatus) -> bool:
        """戻り値: 更新できた場合True、対象行が(他人のtaskも含め)見つからない場合False"""
        async with self._pool.acquire() as conn:
            await conn.begin()
            try:
                now = datetime.now(timezone.utc).replace(tzinfo=None)
                async with conn.cursor() as cur:
                    await cur.execute(
                        "UPDATE tasks SET name = %s, description = %s, status = %s, finished_on = %s, updated_at = %s "
                        "WHERE id = %s AND user_id = %s",
                        (input_.name, input_.description, status.db_value, input_.finished_on, now, task_id, user_id),
                    )
                    updated = cur.rowcount
                if updated == 0:
                    await conn.rollback()
                    return False
                await self._replace_labels(conn, task_id, input_.label_ids, now)
                await conn.commit()
                return True
            except Exception:
                await conn.rollback()
                raise

    async def delete(self, task_id: int, user_id: int) -> bool:
        """tasksとtask_labelsの削除を1つのトランザクションで包む。
        task_labelsには外部キー制約が無い(migrations/000004)ため、トランザクション無しで
        個別にDELETEすると、両文の間でプロセスが落ちた場合にtask_labelsの孤立行が残り得る
        (backend-rust/backend-c/backend-cpp/backend-java/backend-kotlinと同じ設計。backend-rustには
        かつてこの保護が欠けている既知バグがあり、後に修正された経緯がある)
        """
        async with self._pool.acquire() as conn:
            await conn.begin()
            try:
                async with conn.cursor() as cur:
                    await cur.execute("DELETE FROM tasks WHERE id = %s AND user_id = %s", (task_id, user_id))
                    deleted = cur.rowcount
                if deleted == 0:
                    await conn.rollback()
                    return False
                async with conn.cursor() as cur:
                    await cur.execute("DELETE FROM task_labels WHERE task_id = %s", (task_id,))
                await conn.commit()
                return True
            except Exception:
                await conn.rollback()
                raise

    async def find_user_by_id(self, user_id: int) -> User | None:
        """JWT認証のuser_id解決用。ローカル発行issuerのsubはusers.idそのもの、存在確認のみ行う"""
        async with self._pool.acquire() as conn:
            async with conn.cursor(aiomysql.DictCursor) as cur:
                await cur.execute("SELECT id, email, name FROM users WHERE id = %s", (user_id,))
                row = await cur.fetchone()
                return None if row is None else User(id=row["id"], email=row["email"], name=row["name"])

    async def find_user_by_keycloak_sub(self, keycloak_sub: str) -> User | None:
        """Keycloak発行issuerのsub=keycloak_subは、usersテーブルには無くuser_keycloaksテーブルに
        分離されている(migration 000008_split_user_credentials、CONTRACT.mdセクション16.2)ため
        JOIN経由で引く
        """
        async with self._pool.acquire() as conn:
            async with conn.cursor(aiomysql.DictCursor) as cur:
                await cur.execute(
                    "SELECT users.id AS id, users.email AS email, users.name AS name "
                    "FROM users JOIN user_keycloaks ON user_keycloaks.user_id = users.id "
                    "WHERE user_keycloaks.keycloak_sub = %s",
                    (keycloak_sub,),
                )
                row = await cur.fetchone()
                return None if row is None else User(id=row["id"], email=row["email"], name=row["name"])

    async def _replace_labels(self, conn, task_id: int, label_ids: list[int], now: datetime) -> None:
        async with conn.cursor() as cur:
            await cur.execute("DELETE FROM task_labels WHERE task_id = %s", (task_id,))
            # 【他言語で見つかった既知バグと同種】label_idsに同じidが重複して含まれる場合
            # (例: [3,3,5])、重複除去せずそのままINSERTすると2回目の(task_id,3)で
            # task_labelsの(task_id,label_id)へのUNIQUE制約(migrations/000004)に違反し、
            # 生のMySQLエラー(1062 Duplicate entry)がそのまま呼び出し元へ伝播してしまう
            # (backend(Go)・backend-rust・backend-java・backend-kotlinで見つかった同根のバグ)。
            # dict.fromkeys()はKotlinのLinkedHashSet相当(挿入順を保ったまま重複除去する)
            deduped = list(dict.fromkeys(label_ids))
            if deduped:
                await cur.executemany(
                    "INSERT INTO task_labels (task_id, label_id, created_at, updated_at) VALUES (%s, %s, %s, %s)",
                    [(task_id, label_id, now, now) for label_id in deduped],
                )

    async def _attach_labels(self, conn, tasks: list[Task]) -> list[Task]:
        if not tasks:
            return tasks
        ids = [t.id for t in tasks]
        placeholders = ",".join(["%s"] * len(ids))
        by_task: dict[int, list[Label]] = {}
        async with conn.cursor(aiomysql.DictCursor) as cur:
            await cur.execute(
                "SELECT task_labels.task_id AS task_id, labels.id AS id, labels.name AS name "
                f"FROM task_labels JOIN labels ON labels.id = task_labels.label_id "
                f"WHERE task_labels.task_id IN ({placeholders})",
                ids,
            )
            rows = await cur.fetchall()
            for row in rows:
                by_task.setdefault(row["task_id"], []).append(Label(id=row["id"], name=row["name"]))
        return [
            Task(
                id=t.id,
                name=t.name,
                description=t.description,
                status=t.status,
                finished_on=t.finished_on,
                user_id=t.user_id,
                labels=by_task.get(t.id, []),
                created_at=t.created_at,
                updated_at=t.updated_at,
            )
            for t in tasks
        ]

    @staticmethod
    def _row_to_task(row: dict) -> Task:
        status = TaskStatus.from_db_value(row["status"]) or TaskStatus.WAITING
        # 【backend-java/backend-kotlinと対照的、実機検証で確認済み】aiomysqlはDATETIME列を
        # ナイーブな(tzinfoを持たない)datetime.datetimeとして、タイムゾーン変換を一切行わずに
        # そのまま返す。JDBCの`getTimestamp().toLocalDateTime()`がJVMのシステムデフォルト
        # タイムゾーンを経由して変換してしまう(Java/Kotlinで実際に見つかった落とし穴)のとは
        # 異なり、aiomysqlには元から変換ロジックが介在しないため、この種のタイムゾーンずれの
        # バグはPythonでは最初から起こり得ない(README.md「実装時に判明した既知の差異」参照)
        return Task(
            id=row["id"],
            name=row["name"],
            description=row["description"],
            status=status,
            finished_on=row["finished_on"],
            user_id=row["user_id"],
            labels=[],
            created_at=row["created_at"],
            updated_at=row["updated_at"],
        )
