package com.bffgin.backend

import cats.effect.IO
import java.time.{LocalDate, ZoneOffset}

// backend/internal/service/task.go の validateTaskInput をそのまま再現する
//   - name: 必須・20文字以内
//   - finished_on: 必須・過去日不可
//   - status: enumの範囲内
// N+1の意図的再現(Go側v1旧実装の教材)はこの比較実装の主題ではないため行わない
// (CONTRACT.mdセクション20.9)。REST/gRPCとも効率的なクエリで実装する
object TaskService {
  private def parseFinishedOn(raw: String): Either[AppError, LocalDate] =
    scala.util.Try(LocalDate.parse(raw)).toEither.left.map(_ => AppError.InvalidFinishedOn(raw))

  // backend/internal/service/task.go の validateTaskInput をそのまま再現する(純粋関数、テスト容易)
  //   - name: 必須・20文字以内
  //   - finished_on: 必須・過去日不可
  //   - status: enumの範囲内
  // 【テスト監査で発見・修正した実バグ】以前はLocalDate.now()(JVMのデフォルトタイムゾーン
  // =実行環境のOS設定次第で不定)を基準にしていた。finished_onのパース(parseFinishedOn、
  // ISO-8601のLocalDate.parse)自体はタイムゾーンを持たない素の日付のため、比較対象の
  // 「今日」だけがタイムゾーン依存になっているのは非対称かつ不定。さらにこの不定さは
  // Rust/Rails(いずれも明示的にUTC基準)との間で、同一リクエスト・同一時刻に対して
  // 受理/拒否の判定が割れる契約違反(CONTRACT.mdセクション20.5)を引き起こしうる
  // (例: 実行環境がJST(UTC+9)の場合、UTC 15:00〜23:59の間はこの実装だけ「今日」の判定が
  // 他言語より1日進んでしまう)。UTC固定にすることでRust/Railsと同じ基準に揃える
  def validate(in: TaskInput, today: LocalDate = LocalDate.now(ZoneOffset.UTC)): Either[AppError, (TaskStatus, LocalDate)] =
    for {
      _          <- Either.cond(in.name.nonEmpty, (), AppError.Validation("nameは必須です"))
      // 【2回目のテスト監査で修正】JVMのString#lengthはUTF-16コード単位数を返すため、
      // 基本多言語面(BMP)外の文字(U+10000以上、絵文字の多くはここに属する)はサロゲートペア
      // 2単位として数えられてしまう。backend(Go)は`len([]rune(name))`(コードポイント数)で
      // 20文字を判定しているため、`.length`のままだとコードポイント数20の名前(絵文字20文字等)を
      // 誤って拒否してしまう契約不一致になる。`codePointCount`でコードポイント単位に揃える
      // (Scala Pekko実装は元からこの方式だった。http4s実装だけがこの罠を踏んでいた)
      _          <- Either.cond(in.name.codePointCount(0, in.name.length) <= 20, (), AppError.Validation("nameは20文字以内である必要があります"))
      finishedOn <- parseFinishedOn(in.finishedOn)
      _          <- Either.cond(!finishedOn.isBefore(today), (), AppError.Validation("finished_onに過去日は指定できません"))
      status     <- TaskStatus.fromWire(in.status).left.map(msg => AppError.Validation(msg))
    } yield (status, finishedOn)
}

class TaskService(repo: TaskRepo) {
  private def validate(in: TaskInput): Either[AppError, (TaskStatus, LocalDate)] = TaskService.validate(in)

  def list(userId: Long, name: String, status: Option[TaskStatus], labelIds: List[Long], limit: Int, offset: Int)
      : IO[(List[Task], Long)] =
    repo.listOffset(TaskListFilter(userId, name, status, labelIds, limit, offset))

  def listCursor(userId: Long, name: String, status: Option[TaskStatus], labelIds: List[Long], cursor: Long, limit: Int)
      : IO[(List[Task], Long)] =
    repo.listCursor(TaskCursorFilter(userId, name, status, labelIds, cursor, limit)).map { tasks =>
      val nextCursor = if (tasks.length < limit) 0L else tasks.lastOption.map(_.id).getOrElse(0L)
      (tasks, nextCursor)
    }

  def get(id: Long, userId: Long): IO[Task] =
    repo.get(id, userId).flatMap {
      case Some(t) => IO.pure(t)
      case None    => IO.raiseError(AppError.NotFound)
    }

  def create(userId: Long, in: TaskInput): IO[Task] =
    validate(in) match {
      case Left(err) => IO.raiseError(err)
      case Right((status, finishedOn)) =>
        for {
          id   <- repo.create(userId, in, status, finishedOn)
          task <- get(id, userId)
        } yield task
    }

  def update(id: Long, userId: Long, in: TaskInput): IO[Task] =
    validate(in) match {
      case Left(err) => IO.raiseError(err)
      case Right((status, finishedOn)) =>
        for {
          ok   <- repo.update(id, userId, in, status, finishedOn)
          _    <- if (ok) IO.unit else IO.raiseError(AppError.NotFound)
          task <- get(id, userId)
        } yield task
    }

  def delete(id: Long, userId: Long): IO[Unit] =
    repo.delete(id, userId).flatMap {
      case true  => IO.unit
      case false => IO.raiseError(AppError.NotFound)
    }
}
