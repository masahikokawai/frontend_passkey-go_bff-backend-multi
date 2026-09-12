package com.bffgin.backendpekko

import org.scalatest.matchers.should.Matchers
import org.scalatest.wordspec.AnyWordSpec
import slick.jdbc.MySQLProfile.api._

import java.sql.DriverManager
import java.time.LocalDate
import scala.concurrent.Await
import scala.concurrent.ExecutionContext.Implicits.global
import scala.concurrent.duration._

// 【テスト監査で発見された残課題への対応】このセッションの監査で、NUL文字・制御文字を
// 含むTask name/descriptionが実DBへ正しく往復するかをGo(GORM/bob両方)・Rails・
// Scala(http4s)では実機DBで確認済みだったが、Scala(Pekko)だけは
// (backend-scala-pekkoにはこれまでDB接続テストが1つも無く、DELETE冪等性の確認も
// InMemoryTaskRepositoryという偽実装で代替していた)未検証のまま残っていた。
// このファイルで、backend-scala-http4sのTaskRepoDeleteIdempotencyDbSuite.scala/
// TaskNulByteRoundTripDbSuite.scalaと同じ設計(専用の使い捨てDB、TEST_DB_DSNで
// 既定のsbt testからは除外)を踏襲し、backend-scala-pekko側で初めてDBへ実接続する
// テストを追加する
//
// 【安全上の重要な注意】TEST_DB_DSNが指す先(通常はbff_gin_development、ユーザーが
// 手動検証で使っている共有DB)には一切触れない。専用の使い捨てデータベース
// (throwawayDbName)を自分で作成・破棄する
//
// 【実行方法】既定の sbt test ではこのテストはassumeにより自動的にキャンセル(スキップ)
// される(TEST_DB_DSN未設定時)。実際にMySQLへ接続して検証したい場合は、既に起動中の
// MySQL(docker compose up済み)に対して以下のように実行する:
//   TEST_DB_DSN=set sbt "testOnly com.bffgin.backendpekko.TaskNulByteRoundTripDbSpec"
// (このプロジェクトの開発環境は常に127.0.0.1:13306固定のため、TEST_DB_DSNの値自体は
// 「設定されているかどうか」という opt-in フラグとしてのみ使う。他言語のように
// DSN文字列をパースするのではなく、Config.scalaと同じ既定のhost/portを直接使う)
class TaskNulByteRoundTripDbSpec extends AnyWordSpec with Matchers {

  private val throwawayDbName = "bff_gin_scala_pekko_nul_test"
  private val dbHost = "127.0.0.1"
  private val dbPort = 13306

  "SlickTaskRepository" should {
    "round-trip a task name/description containing NUL and control characters without truncation or corruption" in {
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
          // Db.scalaのTasksTable/TaskLabelsTable/LabelsTableの定義と一致させる
          // (Slickのcolumnマッピングと食い違うと実行時エラーになるため)
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
        } finally st.close()
      } finally dataConn.close()

      val db = Database.forURL(url = dbUrl, user = "root", password = "", driver = "com.mysql.cj.jdbc.Driver")
      try {
        val repo = new SlickTaskRepository(db)

        // 制御文字は文字列リテラルへ直接書かず、数値コードポイントから toChar で組み立てる
        // (ソースファイル中にエスケープシーケンス表記を書くと、エディタ/ツール類が意図せず
        // 生の制御バイトへ変換してしまう事故が起きうるため、数値からの組み立てに統一する。
        // backend-scala-http4sのTaskNulByteRoundTripDbSuite.scalaと同じ理由・同じ対策)
        val nulChar: Char = 0
        val sohChar: Char = 1
        val unitSepChar: Char = 31

        // "evil" + NUL + SOH + "name" = 10文字(VARCHAR(20)の範囲内)
        // 末尾の"name"が生きていること自体が、NULでの途中切り詰めが起きていない証拠になる
        val nulName = "evil" + nulChar.toString + sohChar.toString + "name"
        val nulDescription =
          "desc" + nulChar.toString + "with" + sohChar.toString + "control" + unitSepChar.toString + "chars"

        val input = TaskInput(nulName, Some(nulDescription), "waiting", LocalDate.parse("2099-01-01"), Nil)

        val userId = 1L
        val dto = Await.result(
          repo.create(userId, input).flatMap(created => repo.get(created.id, userId)),
          10.seconds
        )

        dto shouldBe defined
        dto.get.name shouldBe nulName
        dto.get.description shouldBe Some(nulDescription)
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
