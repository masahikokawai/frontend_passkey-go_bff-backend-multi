package com.bffgin.backendpekko

import org.scalatest.matchers.should.Matchers
import org.scalatest.wordspec.AnyWordSpec
import slick.jdbc.MySQLProfile.api._

import java.sql.DriverManager
import java.time.LocalDate
import scala.concurrent.Await
import scala.concurrent.ExecutionContext.Implicits.global
import scala.concurrent.duration._

// 【テスト監査(9回目、重複label_idsという角度)で確認】
// Scala(Pekko)のSlickRepository.replaceLabelsは元々`labelIds.distinct`していたため、
// このバグ(Go/Rust/Scala(http4s)/Railsで見つかった、重複label_idを含むcreate/updateが
// task_labelsのUNIQUE制約(migrations/000004)違反で生エラーになる問題)の影響を
// 受けていなかった。ただし実際にDBへ接続してこれを確認するテストがこれまで無かったため、
// TaskNulByteRoundTripDbSpec.scalaと同じDB接続基盤を使い、正しい挙動を回帰テストとして固定する
//
// 【実行方法】既定の sbt test ではこのテストはassumeにより自動的にキャンセル(スキップ)される
// (TEST_DB_DSN未設定時)。実際にMySQLへ接続して検証したい場合は、既に起動中の
// MySQL(docker compose up済み)に対して以下のように実行する:
//   TEST_DB_DSN=set sbt "testOnly com.bffgin.backendpekko.TaskDuplicateLabelIdDbSpec"
class TaskDuplicateLabelIdDbSpec extends AnyWordSpec with Matchers {

  private val throwawayDbName = "bff_gin_scala_pekko_duplabel_test"
  private val dbHost = "127.0.0.1"
  private val dbPort = 13306

  "SlickTaskRepository" should {
    "dedupe a duplicate label id in create/update instead of raising a raw DB error" in {
      assume(
        sys.env.contains("TEST_DB_DSN"),
        "TEST_DB_DSN未設定のためこのDB接続テストはスキップします(このファイル冒頭のコメント参照)"
      )

      val serverUrl = s"jdbc:mysql://$dbHost:$dbPort/?serverTimezone=UTC"
      Class.forName("com.mysql.cj.jdbc.Driver")
      val setupConn = DriverManager.getConnection(serverUrl, "root", "")
      try {
        val st = setupConn.createStatement()
        try st.execute(s"CREATE DATABASE IF NOT EXISTS $throwawayDbName")
        finally st.close()
      } finally setupConn.close()

      val dbUrl = s"jdbc:mysql://$dbHost:$dbPort/$throwawayDbName?serverTimezone=UTC"
      val dataConn = DriverManager.getConnection(dbUrl, "root", "")
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
          st.execute("""CREATE TABLE IF NOT EXISTS task_labels (
              id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
              task_id    BIGINT UNSIGNED NOT NULL,
              label_id   BIGINT UNSIGNED NOT NULL,
              created_at DATETIME        NOT NULL,
              updated_at DATETIME        NOT NULL,
              PRIMARY KEY (id),
              UNIQUE KEY index_task_label_on_uniq_key (task_id, label_id)
            ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4""")
          st.execute("""CREATE TABLE IF NOT EXISTS labels (
              id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
              name       VARCHAR(20)     NOT NULL,
              created_at DATETIME        NOT NULL,
              updated_at DATETIME        NOT NULL,
              PRIMARY KEY (id)
            ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4""")
          st.execute("INSERT INTO labels (id, name, created_at, updated_at) VALUES (1, 'dup-test-label', NOW(), NOW())")
        } finally st.close()
      } finally dataConn.close()

      val db = Database.forURL(url = dbUrl, user = "root", password = "", driver = "com.mysql.cj.jdbc.Driver")
      try {
        val repo = new SlickTaskRepository(db)
        val userId = 1L
        val labelId = 1L
        val input = TaskInput("重複label_idテスト", None, "waiting", LocalDate.parse("2099-01-01"), Seq(labelId, labelId))

        val created = Await.result(repo.create(userId, input), 10.seconds)
        val updated = Await.result(
          repo.update(created.id, userId, input.copy(labelIds = Seq(labelId, labelId))),
          10.seconds
        )
        updated shouldBe defined

        val countQuery = sql"SELECT COUNT(*) FROM task_labels WHERE task_id = ${created.id} AND label_id = $labelId".as[Long].head
        val count = Await.result(db.run(countQuery), 10.seconds)
        count shouldBe 1L
      } finally {
        db.close()
      }

      val cleanupConn = DriverManager.getConnection(serverUrl, "root", "")
      try {
        val st = cleanupConn.createStatement()
        try st.execute(s"DROP DATABASE IF EXISTS $throwawayDbName")
        finally st.close()
      } finally cleanupConn.close()
    }
  }
}
