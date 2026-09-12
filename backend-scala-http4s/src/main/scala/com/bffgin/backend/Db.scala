package com.bffgin.backend

import cats.effect.{IO, Resource}
import doobie.hikari.HikariTransactor
import doobie.util.ExecutionContexts

object Db {
  // backend/internal/config/config.go の DBDSN(Go MySQLドライバ形式)からJDBC接続情報へ変換する
  // (既存Go実装・他言語実装すべてが同じMySQLインスタンス・同じテーブルへ接続する
  //  マイグレーションはbackendのgolang-migrateが正本のため、ここでは接続するだけ)
  def transactor(cfg: Config): Resource[IO, HikariTransactor[IO]] = {
    val (jdbcUrl, user, pass) = Config.toJdbcUrl(cfg.dbDsn)
    for {
      ce <- ExecutionContexts.fixedThreadPool[IO](8)
      xa <- HikariTransactor.newHikariTransactor[IO](
        "com.mysql.cj.jdbc.Driver",
        jdbcUrl,
        user,
        pass,
        ce
      )
    } yield xa
  }
}
