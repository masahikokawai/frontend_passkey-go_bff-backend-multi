package com.bffgin.backend

import cats.effect.{IO, Ref, Resource}
import cats.effect.syntax.all._
import cats.syntax.all._
import scala.concurrent.duration._

/** backend.external-tasks-pagination-v2 の評価結果を10秒間隔でキャッシュする
  * Go実装(cmd/server/main.goのfeatureflag.NewMySQLEvaluator、pollInterval既定10秒)と
  * 同じ考え方: backend自身がMySQLのfeature_flagsテーブルを正本として直接評価し、
  * bffやgatewayのような外部ポーリング(HTTP export)は使わない
  */
final class ExternalFlags private (ref: Ref[IO, Boolean]) {
  def paginationV2: IO[Boolean] = ref.get
}

object ExternalFlags {
  val FlagKey = "backend.external-tasks-pagination-v2"
  private val PollInterval = 10.seconds

  def resource(repo: TaskRepo): Resource[IO, ExternalFlags] =
    for {
      initial <- Resource.eval(repo.readBoolFlag(FlagKey))
      ref     <- Resource.eval(Ref.of[IO, Boolean](initial))
      _ <- (IO.sleep(PollInterval) >> repo.readBoolFlag(FlagKey).attempt.flatMap {
             case Right(v) => ref.set(v)
             case Left(_)  => IO.unit // 一時的なDB障害等ではキャッシュ値を維持する
           }).foreverM.background
    } yield new ExternalFlags(ref)
}
