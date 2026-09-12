package com.bffgin.backend

import cats.effect.IO
import cats.effect.unsafe.implicits.global
import doobie._
import doobie.implicits._
import java.sql.DriverManager
import java.time.LocalDate

// 【テスト監査(9回目、重複label_idsという角度)で発見・修正した実バグ】
// 同じlabel_idを複数回渡すと(例: [3,3,5])、TaskRepo.create/updateが重複除去せず
// そのままtraverseでINSERTしていたため、2回目のINSERTでtask_labelsの
// (task_id,label_id)へのUNIQUE制約(migrations/000004)に違反し、生のMySQLエラーが
// 伝播していた(Go/Rust実装で見つかった同種のバグと同じ根本原因)。
// TaskRepo.scalaのcreate/updateに`.distinct`を追加して修正、このテストで固定する。
//
// DBテスト基盤・実行方法はTaskRepoDeleteIdempotencyDbSuite.scalaと全く同じ
// (専用の使い捨てDBを使い、共有DB(bff_gin_development)には一切触れない)
class TaskRepoDuplicateLabelIdDbSuite extends munit.FunSuite {

  private val throwawayDbName = "bff_gin_scala_http4s_test"

  private def dsnWithThrowawayDb(dsn: String): String = {
    val re = "/[^/?]+(\\?|$)".r
    re.replaceAllIn(dsn, m => "/" + throwawayDbName + (if (m.group(1) == null) "" else m.group(1)))
  }

  test("重複label_idを渡してもcreate/updateがエラーにならず、重複除去して1件だけ紐づく") {
    val dsnOpt = sys.env.get("TEST_DB_DSN")
    assume(dsnOpt.isDefined, "TEST_DB_DSN未設定のためこのDB接続テストはスキップします")
    val dsn = dsnWithThrowawayDb(dsnOpt.get)
    val (jdbcUrl, user, pass) = Config.toJdbcUrl(dsn)
    val serverUrl = jdbcUrl.replaceFirst("/" + throwawayDbName, "/")

    Class.forName("com.mysql.cj.jdbc.Driver")
    val setupConn = DriverManager.getConnection(serverUrl, user, pass)
    try {
      val st = setupConn.createStatement()
      try st.execute(s"CREATE DATABASE IF NOT EXISTS $throwawayDbName")
      finally st.close()
    } finally setupConn.close()

    val dataConn = DriverManager.getConnection(jdbcUrl, user, pass)
    try {
      val st = dataConn.createStatement()
      try {
        st.execute("""CREATE TABLE IF NOT EXISTS tasks (
            id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
            name        VARCHAR(20)     NOT NULL,
            description TEXT            NULL,
            status      TINYINT UNSIGNED NOT NULL DEFAULT 1,
            finished_on DATE            NOT NULL,
            user_id     BIGINT UNSIGNED NOT NULL,
            created_at  DATETIME        NOT NULL,
            updated_at  DATETIME        NOT NULL,
            PRIMARY KEY (id)
          ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4""")
        st.execute("""CREATE TABLE IF NOT EXISTS labels (
            id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
            name       VARCHAR(255)    NOT NULL,
            created_at DATETIME        NOT NULL,
            updated_at DATETIME        NOT NULL,
            PRIMARY KEY (id)
          ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4""")
        st.execute("""CREATE TABLE IF NOT EXISTS task_labels (
            id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
            task_id    BIGINT UNSIGNED NOT NULL,
            label_id   BIGINT UNSIGNED NOT NULL,
            created_at DATETIME        NOT NULL,
            updated_at DATETIME        NOT NULL,
            PRIMARY KEY (id),
            UNIQUE KEY index_task_label_on_uniq_key (task_id, label_id)
          ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4""")
        st.execute("DELETE FROM task_labels")
        st.execute("DELETE FROM labels")
        st.execute("DELETE FROM tasks")
        st.execute("INSERT INTO labels (id, name, created_at, updated_at) VALUES (1, 'dup-test-label', NOW(), NOW())")
      } finally st.close()
    } finally dataConn.close()

    val userId = 1L
    val labelId = 1L
    val createInput = TaskInput("重複label_idテスト", None, "waiting", "2099-01-01", List(labelId, labelId))

    val (taskId, taskLabelCount) = Db.transactor(Config.load().copy(dbDsn = dsn)).use { xa =>
      val repo = new TaskRepo(xa)
      for {
        id <- repo.create(userId, createInput, TaskStatus.Waiting, LocalDate.parse("2099-01-01"))
        // update経路でも同様に確認する
        _  <- repo.update(id, userId, createInput.copy(labelIds = List(labelId, labelId)), TaskStatus.Waiting, LocalDate.parse("2099-01-01"))
        count <- sql"SELECT COUNT(*) FROM task_labels WHERE task_id = $id AND label_id = $labelId".query[Long].unique.transact(xa)
      } yield (id, count)
    }.unsafeRunSync()

    assertEquals(taskLabelCount, 1L, s"task_labels行が重複除去されず${taskLabelCount}件ある(1件になるべき)")
    assert(taskId > 0, "タスクが正しく作成されていること")

    val cleanupConn = DriverManager.getConnection(serverUrl, user, pass)
    try {
      val st = cleanupConn.createStatement()
      try st.execute(s"DROP DATABASE IF EXISTS $throwawayDbName")
      finally st.close()
    } finally cleanupConn.close()
  }
}
