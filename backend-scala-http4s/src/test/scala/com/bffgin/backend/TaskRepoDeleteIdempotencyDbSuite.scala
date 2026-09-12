package com.bffgin.backend

import cats.effect.IO
import cats.effect.unsafe.implicits.global
import java.sql.DriverManager
import java.time.LocalDate

// 【テスト監査で発見された残課題への対応】backend-scala-http4sには実DBに接続するテストが
// これまで1つも無く(48件全てpure/mockのユニットテスト)、DELETE操作の冪等性
// (2回目の削除がGo/Rust/Scala(Pekko)/Railsと同じくクリーンな404相当になるか、
// CONTRACT.mdセクション23.1参照)が未検証のまま残っていた。このファイルで
// TaskRepoを実際のMySQLへ接続して検証する、このプロジェクト初のDB接続テストを追加する
//
// 【安全上の重要な注意、backend/test/integration/migration_rollback_test.goと同じ設計】
// TEST_DB_DSNが指す先(通常はbff_gin_development、ユーザーが手動検証で使っている
// 共有DB)を直接使うと、既存データを壊しかねない。そのためこのテストはTEST_DB_DSNから
// 接続情報(ホスト/ポート/ユーザー/パスワード)だけを借りて、専用の使い捨てデータベース
// (throwawayDbName)を自分で作成・破棄し、共有DBのスキーマ・データには一切触れない
//
// 【実行方法】既定の`sbt test`ではこのテストはassumeにより自動的にスキップされる
// (TEST_DB_DSN未設定時)。実際にMySQLへ接続して検証したい場合は、既に起動中の
// MySQL(docker compose up済み)に対して以下のように実行する:
//   TEST_DB_DSN="root@tcp(127.0.0.1:13306)/bff_gin_development?parseTime=true" \
//     sbt "testOnly com.bffgin.backend.TaskRepoDeleteIdempotencyDbSuite"
// (dbname部分はどうせ使い捨てDB名へ置き換えるため、実際にどのdbnameを指定しても良い。
// host/port/user/passだけが使われる)
class TaskRepoDeleteIdempotencyDbSuite extends munit.FunSuite {

  private val throwawayDbName = "bff_gin_scala_http4s_test"

  // GoのDSN形式("user:pass@tcp(host:port)/dbname?params")のdbname部分だけを
  // throwawayDbNameへ置き換える(backend/test/integration/migration_rollback_test.goの
  // dsnWithDBNameと同じ発想)
  private def dsnWithThrowawayDb(dsn: String): String = {
    val re = "/[^/?]+(\\?|$)".r
    re.replaceAllIn(dsn, m => "/" + throwawayDbName + (if (m.group(1) == null) "" else m.group(1)))
  }

  test("2回目のDELETEはfalse(=NotFound相当)を返し、1回目とは異なる挙動になる(冪等性確認)") {
    val dsnOpt = sys.env.get("TEST_DB_DSN")
    // 【既定のsbt testではここでskip】TEST_DB_DSN未設定時はDBが無い前提のため、
    // munitのassumeでこのテストだけを安全にスキップする(失敗扱いにしない)
    assume(dsnOpt.isDefined, "TEST_DB_DSN未設定のためこのDB接続テストはスキップします(このファイル冒頭のコメント参照)")
    val dsn = dsnWithThrowawayDb(dsnOpt.get)
    val (jdbcUrl, user, pass) = Config.toJdbcUrl(dsn)
    // dbname無しのURLでサーバーへ接続し、使い捨てDB自体を作成する
    // (CREATE DATABASEはDB名を指定した接続では実行できない/不要なため、素のJDBCで行う)
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
        // tasks/task_labelsのみ(TaskRepo.deleteが実際に触れる2テーブル)。
        // usersへの外部キー制約は本家migrations(backend/migrations/000003)にも
        // 存在しないため、ここでも省略して良い
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
      } finally st.close()
    } finally dataConn.close()

    val userId = 1L
    val input = TaskInput("削除冪等性テスト", None, "waiting", "2099-01-01", Nil)

    val result = Db.transactor(Config.load().copy(dbDsn = dsn)).use { xa =>
      val repo = new TaskRepo(xa)
      for {
        id     <- repo.create(userId, input, TaskStatus.Waiting, LocalDate.parse("2099-01-01"))
        first  <- repo.delete(id, userId)
        second <- repo.delete(id, userId)
      } yield (first, second)
    }.unsafeRunSync()

    assertEquals(result._1, true, "1回目のDELETEは実際に行を削除するのでtrue(=成功)のはず")
    assertEquals(result._2, false, "2回目のDELETEは既に存在しない行が対象なのでfalse(=NotFound相当)のはず。" +
      "trueが返るなら、削除済みの行を再度「削除できた」と誤って報告する回帰バグ")

    // 後片付け: 使い捨てDBを破棄する(共有DBには一切触れていないため、
    // 破棄し忘れても実害は無いが、行儀として消しておく)
    val cleanupConn = DriverManager.getConnection(serverUrl, user, pass)
    try {
      val st = cleanupConn.createStatement()
      try st.execute(s"DROP DATABASE IF EXISTS $throwawayDbName")
      finally st.close()
    } finally cleanupConn.close()
  }
}
