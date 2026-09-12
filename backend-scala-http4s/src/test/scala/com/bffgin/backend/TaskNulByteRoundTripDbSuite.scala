package com.bffgin.backend

import cats.effect.unsafe.implicits.global
import java.sql.DriverManager
import java.time.LocalDate

// 【テスト監査で発見された残課題への対応】このセッションの監査で、NUL文字・制御文字を
// 含むTask name/descriptionが実DBへ正しく往復するかをGo(GORM/bob両方)・Railsでは
// 実機DBで確認済みだったが、Rust/Scala(http4s)/Scala(Pekko)は時間の都合で
// 未検証のまま残っていた。ここでbackend-scala-http4sについて確認する
//
// 【安全上の重要な注意】TaskRepoDeleteIdempotencyDbSuite.scalaと全く同じ設計を踏襲する:
// TEST_DB_DSNからは接続情報(ホスト/ポート/ユーザー/パスワード)だけを借り、
// 専用の使い捨てデータベース(throwawayDbName)を自分で作成・破棄し、共有DB
// (bff_gin_development)のスキーマ・データには一切触れない
//
// 【実行方法】既定の sbt test ではこのテストはassumeにより自動的にスキップされる
// (TEST_DB_DSN未設定時)。実際にMySQLへ接続して検証したい場合は、既に起動中の
// MySQL(docker compose up済み)に対して以下のように実行する:
//   TEST_DB_DSN=(root at tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true) \
//     sbt "testOnly com.bffgin.backend.TaskNulByteRoundTripDbSuite"
class TaskNulByteRoundTripDbSuite extends munit.FunSuite {

  private val throwawayDbName = "bff_gin_scala_http4s_nul_test"

  private def dsnWithThrowawayDb(dsn: String): String = {
    val re = "/[^/?]+([?]|$)".r
    re.replaceAllIn(dsn, m => "/" + throwawayDbName + (if (m.group(1) == null) "" else m.group(1)))
  }

  test("NUL文字・制御文字を含むtask name/descriptionが実DBへ切り詰め・破損無く往復する") {
    val dsnOpt = sys.env.get("TEST_DB_DSN")
    assume(dsnOpt.isDefined, "TEST_DB_DSN未設定のためこのDB接続テストはスキップします(このファイル冒頭のコメント参照)")
    val dsn = dsnWithThrowawayDb(dsnOpt.get)
    val (jdbcUrl, user, pass) = Config.toJdbcUrl(dsn)
    val serverUrl = jdbcUrl.replaceFirst("/" + throwawayDbName, "/")

    Class.forName("com.mysql.cj.jdbc.Driver")
    val setupConn = DriverManager.getConnection(serverUrl, user, pass)
    try {
      val st = setupConn.createStatement()
      try {
        st.execute(s"CREATE DATABASE IF NOT EXISTS $throwawayDbName")
      } finally st.close()
    } finally setupConn.close()

    val dataConn = DriverManager.getConnection(jdbcUrl, user, pass)
    try {
      val st = dataConn.createStatement()
      try {
        // TaskRepoDeleteIdempotencyDbSuite.scalaと同じ最小スキーマ(名前・説明の
        // 型・文字コードだけがこのテストの本題なので、他の列は同じ定義を流用する)
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
        // TaskRepoDeleteIdempotencyDbSuite.scalaには無かったが、TaskRepo.get/create経由の
        // labelsForTask(id/nameのみ参照)がJOIN先として要求するため、このテストでは追加する
        st.execute("""CREATE TABLE IF NOT EXISTS labels (
            id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
            name       VARCHAR(20)     NOT NULL,
            user_id    BIGINT UNSIGNED NOT NULL,
            created_at DATETIME        NOT NULL,
            updated_at DATETIME        NOT NULL,
            PRIMARY KEY (id)
          ) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4""")
      } finally st.close()
    } finally dataConn.close()

    val userId = 1L

    // 制御文字は文字列リテラルへ直接書かず、数値コードポイントから toChar で組み立てる
    // (ソースファイル中にエスケープシーケンス表記を書くと、エディタ/ツール類が意図せず
    // 生の制御バイトへ変換してしまう事故が起きうるため、数値からの組み立てに統一する)
    val nulChar: Char = 0
    val sohChar: Char = 1
    val unitSepChar: Char = 31

    // "evil" + NUL + SOH + "name" = 10文字(VARCHAR(20)の範囲内)
    // 末尾の"name"が生きていること自体が、NULでの途中切り詰めが起きていない証拠になる
    val nulName = "evil" + nulChar.toString + sohChar.toString + "name"
    val nulDescription = "desc" + nulChar.toString + "with" + sohChar.toString + "control" + unitSepChar.toString + "chars"

    val input = TaskInput(nulName, Some(nulDescription), "waiting", "2099-01-01", Nil)

    val result = Db.transactor(Config.load().copy(dbDsn = dsn)).use { xa =>
      val repo = new TaskRepo(xa)
      for {
        id      <- repo.create(userId, input, TaskStatus.Waiting, LocalDate.parse("2099-01-01"))
        fetched <- repo.get(id, userId)
      } yield fetched
    }.unsafeRunSync()

    val task = result.getOrElse(fail("作成した直後のタスクがgetで見つからない"))
    assertEquals(task.name, nulName, "NUL/制御文字入りのtask nameが実DBへの保存・読み出しで切り詰め・破損している")
    assertEquals(
      task.description,
      Some(nulDescription),
      "NUL/制御文字入りのdescriptionが実DBへの保存・読み出しで切り詰め・破損している"
    )

    val cleanupConn = DriverManager.getConnection(serverUrl, user, pass)
    try {
      val st = cleanupConn.createStatement()
      try st.execute(s"DROP DATABASE IF EXISTS $throwawayDbName")
      finally st.close()
    } finally cleanupConn.close()
  }
}
