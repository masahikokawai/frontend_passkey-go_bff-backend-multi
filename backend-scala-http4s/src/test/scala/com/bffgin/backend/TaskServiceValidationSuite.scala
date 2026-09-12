package com.bffgin.backend

import java.time.{LocalDate, ZoneOffset}

// backend/internal/service/task_test.go(validateTaskInput相当)のケースをそのまま再現する
class TaskServiceValidationSuite extends munit.FunSuite {
  private val today = LocalDate.of(2026, 9, 9)
  private def input(
      name: String = "買い物",
      status: String = "waiting",
      finishedOn: String = "2026-09-10"
  ) = TaskInput(name, None, status, finishedOn, Nil)

  test("正常な入力はRight") {
    val result = TaskService.validate(input(), today)
    assert(result.isRight)
  }

  test("name空文字は必須エラー") {
    val result = TaskService.validate(input(name = ""), today)
    assertEquals(result, Left(AppError.Validation("nameは必須です")))
  }

  test("nameが20文字ちょうどはOK、21文字はエラー") {
    val ok = TaskService.validate(input(name = "a" * 20), today)
    assert(ok.isRight)
    val ng = TaskService.validate(input(name = "a" * 21), today)
    assertEquals(ng, Left(AppError.Validation("nameは20文字以内である必要があります")))
  }

  // 【2回目のテスト監査で追加】backend(Go)は`len([]rune(name))`(コードポイント数)で20文字を
  // 判定している(backend/internal/service/task.go)。JVMのString#lengthはUTF-16コード単位数を
  // 返すため、基本多言語面(BMP)外の文字(U+10000以上、😀等の絵文字を含む多くの絵文字はここに属する)は
  // サロゲートペア2単位として数えられ、コードポイント数の2倍になる。そのため「絵文字20文字」の
  // ようなコードポイント数20の名前が、String#lengthでは40と判定されて誤って拒否されうる。
  // (Scala Pekko実装は`codePointCount`を使っており、この問題を踏んでいない。http4s実装だけが
  // `.length`をそのまま使っていたため、ここでGo/Pekko実装との契約不一致が発覚した)
  test("BMP外の文字(絵文字)20文字はOK(コードポイント数で判定、UTF-16コード単位数ではない)") {
    val emojiName = "😀" * 20 // 😀(U+1F600)を20回、UTF-16コード単位では40単位
    val ok = TaskService.validate(input(name = emojiName), today)
    assert(ok.isRight, s"コードポイント数20のはずが拒否された: $ok")
  }

  test("BMP外の文字(絵文字)21文字はエラー") {
    val emojiName = "😀" * 21
    val ng = TaskService.validate(input(name = emojiName), today)
    assertEquals(ng, Left(AppError.Validation("nameは20文字以内である必要があります")))
  }

  test("finished_onが今日より過去はエラー") {
    val result = TaskService.validate(input(finishedOn = "2026-09-08"), today)
    assertEquals(result, Left(AppError.Validation("finished_onに過去日は指定できません")))
  }

  test("finished_onが今日ちょうどはOK(過去日ではない)") {
    val result = TaskService.validate(input(finishedOn = "2026-09-09"), today)
    assert(result.isRight)
  }

  test("finished_onがパース不能な形式はInvalidFinishedOn") {
    val result = TaskService.validate(input(finishedOn = "not-a-date"), today)
    assertEquals(result, Left(AppError.InvalidFinishedOn("not-a-date")))
  }

  // 【テスト監査で発見・修正した実バグの回帰テスト】
  // todayを明示的に注入する上記の全テストは、validateのデフォルト引数
  // (LocalDate.now(ZoneOffset.UTC))自体を一度も経由していなかった。デフォルト引数が
  // 実行環境のタイムゾーンに依存しないUTC基準であることを、実際に今日の日付
  // (UTC基準)で検証する。以前はLocalDate.now()(JVMのデフォルトタイムゾーン依存)
  // だったため、実行環境がUTC以外(例: JST)の場合にこのテストは失敗していたはずである
  test("デフォルト引数(today未指定)はUTC基準の今日を使う") {
    val utcToday = LocalDate.now(ZoneOffset.UTC)
    val acceptedResult = TaskService.validate(input(finishedOn = utcToday.toString))
    assert(acceptedResult.isRight, s"UTC基準の今日が過去日として拒否された: $acceptedResult")

    val yesterday = utcToday.minusDays(1)
    val rejectedResult = TaskService.validate(input(finishedOn = yesterday.toString))
    assertEquals(rejectedResult, Left(AppError.Validation("finished_onに過去日は指定できません")))
  }

  test("statusが不正な値はValidationエラー(invalid_statusではない、Create/Updateの契約)") {
    val result = TaskService.validate(input(status = "bogus"), today)
    assert(result.isLeft)
    result.left.foreach {
      case AppError.Validation(_) => // OK
      case other                  => fail(s"Validationエラーを期待したが $other だった")
    }
  }
}
