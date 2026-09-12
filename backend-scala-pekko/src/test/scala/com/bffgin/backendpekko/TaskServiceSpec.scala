package com.bffgin.backendpekko

import com.bffgin.backendpekko.auth.{Claims, JwtIssuers}
import org.scalatest.matchers.should.Matchers
import org.scalatest.wordspec.AnyWordSpec

import java.time.{LocalDate, ZoneOffset}
import scala.concurrent.ExecutionContext.Implicits.global
import scala.concurrent.Await
import scala.concurrent.duration._

// backend/internal/service/task_test.go 相当。実DB無しでInMemoryTaskRepository/
// InMemoryUserRepositoryに対して業務ロジック(バリデーション・resolveUserId)を検証する
class TaskServiceSpec extends AnyWordSpec with Matchers {

  private def newService(): (TaskService, InMemoryUserRepository) = {
    val users = new InMemoryUserRepository
    val tasks = new InMemoryTaskRepository
    (new TaskService(tasks, users), users)
  }

  private def await[T](f: scala.concurrent.Future[T]): T = Await.result(f, 5.seconds)

  "TaskService.validate" should {
    "nameが空なら Validation を返す" in {
      val (svc, _) = newService()
      svc.validate(TaskInput("", None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty)) shouldBe a[Left[_, _]]
    }

    "nameが21文字以上なら Validation を返す" in {
      val (svc, _) = newService()
      val longName = "あ" * 21
      svc.validate(TaskInput(longName, None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty)) shouldBe a[Left[_, _]]
    }

    "nameがちょうど20文字なら成功する" in {
      val (svc, _) = newService()
      val name20 = "あ" * 20
      svc.validate(TaskInput(name20, None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty)) shouldBe Right(())
    }

    // 【2回目のテスト監査で追加】backend(Go)は`len([]rune(name))`(コードポイント数)で20文字を
    // 判定している。JVMのString#lengthはUTF-16コード単位数を返すため、基本多言語面外の文字
    // (絵文字等)ではコードポイント数と一致しない(実際にbackend-scala-http4sの`.length`直接使用が
    // この不一致を踏んでいるのが2回目の監査で発覚、`TaskService.scala`参照)。このPekko実装は
    // 元から`codePointCount`を使っており正しいはずだが、この境界値がテストされていなかったため
    // 明示的に確認する
    "基本多言語面外の文字(絵文字)20文字なら成功する(UTF-16コード単位数ではなくコードポイント数で判定)" in {
      val (svc, _) = newService()
      val emojiName20 = "😀" * 20
      svc.validate(TaskInput(emojiName20, None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty)) shouldBe Right(())
    }

    "基本多言語面外の文字(絵文字)21文字なら Validation を返す" in {
      val (svc, _) = newService()
      val emojiName21 = "😀" * 21
      svc.validate(TaskInput(emojiName21, None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty)) shouldBe a[Left[_, _]]
    }

    "finished_onが過去日なら Validation を返す" in {
      val (svc, _) = newService()
      svc.validate(TaskInput("task", None, TaskStatus.Waiting, LocalDate.now().minusDays(1), Seq.empty)) shouldBe a[Left[_, _]]
    }

    "statusが不明な値なら Validation を返す" in {
      val (svc, _) = newService()
      svc.validate(TaskInput("task", None, "bogus", LocalDate.now().plusDays(1), Seq.empty)) shouldBe a[Left[_, _]]
    }

    "全て妥当なら成功する" in {
      val (svc, _) = newService()
      svc.validate(TaskInput("task", None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty)) shouldBe Right(())
    }

    // 【テスト監査で発見・修正した実バグの回帰テスト】
    // 上記の全テストは`LocalDate.now()`(JVMのデフォルトタイムゾーン依存)で日付を作っており、
    // validateのtodayデフォルト引数(修正後はLocalDate.now(ZoneOffset.UTC))が実際に
    // UTC基準であることは検証していなかった。実行環境のタイムゾーンに依存しないことを、
    // UTC基準の「今日ちょうど」「今日から1日過去」で明示的に確認する
    // (修正前はLocalDate.now()=JVMデフォルトゾーン依存だったため、実行環境がUTC以外
    // (例: JST)の場合にこのテストは失敗していたはずである)
    "todayデフォルト引数はUTC基準である(実行環境のタイムゾーンに依存しない)" in {
      val (svc, _) = newService()
      val utcToday = LocalDate.now(ZoneOffset.UTC)
      svc.validate(TaskInput("task", None, TaskStatus.Waiting, utcToday, Seq.empty)) shouldBe Right(())
      svc.validate(TaskInput("task", None, TaskStatus.Waiting, utcToday.minusDays(1), Seq.empty)) shouldBe a[Left[_, _]]
    }
  }

  "TaskService.resolveUserId" should {
    "ローカル発行issuerかつsubがusersに存在すれば内部user_idを解決する" in {
      val (svc, users) = newService()
      users.seed(UserDTO(id = 7, keycloakSub = "", email = "a@example.com", name = "A", role = 1))
      val claims = Claims(issuer = JwtIssuers.LocalHmac, subject = "7", audience = Set("backend"))
      await(svc.resolveUserId(claims)) shouldBe Right(7L)
    }

    "ローカル発行issuerでもsubに対応するuserが無ければUserNotProvisioned" in {
      val (svc, _) = newService()
      val claims = Claims(issuer = JwtIssuers.LocalRsa, subject = "999", audience = Set("backend"))
      await(svc.resolveUserId(claims)) shouldBe Left(ServiceError.UserNotProvisioned)
    }

    "ローカル発行issuerでもsubが数値でなければUserNotProvisioned" in {
      val (svc, _) = newService()
      val claims = Claims(issuer = JwtIssuers.LocalHmac, subject = "not-a-number", audience = Set("backend"))
      await(svc.resolveUserId(claims)) shouldBe Left(ServiceError.UserNotProvisioned)
    }

    "Keycloak発行issuerならkeycloak_subで検索する" in {
      val (svc, users) = newService()
      users.seed(UserDTO(id = 3, keycloakSub = "kc-sub-abc", email = "b@example.com", name = "B", role = 1))
      val claims = Claims(issuer = "http://localhost:8082/realms/training", subject = "kc-sub-abc", audience = Set("backend"))
      await(svc.resolveUserId(claims)) shouldBe Right(3L)
    }

    "Keycloak発行issuerでkeycloak_subが未登録ならUserNotProvisioned" in {
      val (svc, _) = newService()
      val claims = Claims(issuer = "http://localhost:8082/realms/training", subject = "unknown-sub", audience = Set("backend"))
      await(svc.resolveUserId(claims)) shouldBe Left(ServiceError.UserNotProvisioned)
    }

    // 【セキュリティ監査で追加】resolveUserIdはClaims.azpを一切参照していない。そのため
    // 外部公開API用のClient Credentials Grantトークン(external-api-clientが取得する、
    // aud=backendが注入されたトークン)は、署名検証・issuer検証等の「認証」自体は
    // 内部API(REST/gRPC双方)も通過してしまう。これが安全なのは、external-api-client
    // 自身のサービスアカウントsub(Keycloakの慣例で"service-account-external-api-client"
    // のような形式になる)が、JITプロビジョニング(通常ユーザーのログイン時のみ実行される)
    // を一度も経ておらずusersテーブルに該当行が存在しないため、上のテストと同じ
    // 「Keycloak発行issuerでkeycloak_subが未登録→UserNotProvisioned」という経路で
    // 必ず拒否されるという「2段構えの安全性」に依存しているからである。
    // このテストはその安全性を明示的に固定する(Go・Rust・Scala(http4s)の各実装にも
    // 同じ観点のテストを追加済み)。azpに実際の値を設定しても判定に一切影響しない
    // (subだけで拒否される)ことも合わせて確認する。
    "外部公開API用トークン(external-api-clientのサービスアカウント)は内部APIのuser解決に失敗する" in {
      val (svc, _) = newService() // usersテーブルには何も登録しない(JIT未実行を再現)
      val claims = Claims(
        issuer = "http://localhost:8082/realms/training",
        subject = "service-account-external-api-client",
        audience = Set("backend"),
        azp = Some("external-api-client")
      )
      await(svc.resolveUserId(claims)) shouldBe Left(ServiceError.UserNotProvisioned)
    }
  }

  "TaskService CRUD" should {
    "createはバリデーション成功時にNotFoundにならず生成される" in {
      val (svc, _) = newService()
      val result = await(svc.create(1L, TaskInput("task", None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty)))
      result.isRight shouldBe true
      result.toOption.get.userId shouldBe 1L
    }

    "createはバリデーション失敗時にValidationを返す(DBへは到達しない)" in {
      val (svc, _) = newService()
      val result = await(svc.create(1L, TaskInput("", None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty)))
      result shouldBe a[Left[_, _]]
    }

    "getは存在しないIDでNotFoundを返す" in {
      val (svc, _) = newService()
      await(svc.get(999L, 1L)) shouldBe Left(ServiceError.NotFound)
    }

    "updateは他人のtaskをNotFound扱いする(認可漏れ防止)" in {
      val (svc, _) = newService()
      val created = await(svc.create(1L, TaskInput("task", None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty))).toOption.get
      val result = await(svc.update(created.id, 2L, TaskInput("task2", None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty)))
      result shouldBe Left(ServiceError.NotFound)
    }

    "deleteは存在しないIDでNotFoundを返す" in {
      val (svc, _) = newService()
      await(svc.delete(999L, 1L)) shouldBe Left(ServiceError.NotFound)
    }

    // 【6回目のテスト監査(DELETE冪等性のクロス言語パリティ角度)で追加】
    // 既に削除済みのtask idへ再度deleteを呼んでも、1回目と同じNotFoundに一貫してなる
    // ことを確認する(InMemoryTaskRepository.deleteはstore.get(id).filter(...)がNoneなら
    // falseを返す実装のため、削除済み後の呼び出しも「見つからない」として扱われるはず)
    "deleteは既に削除済みのIDへの2回目の呼び出しでもNotFoundを返す(冪等)" in {
      val (svc, _) = newService()
      val created = await(svc.create(1L, TaskInput("task", None, TaskStatus.Waiting, LocalDate.now().plusDays(1), Seq.empty))).toOption.get
      await(svc.delete(created.id, 1L)) shouldBe Right(())
      await(svc.delete(created.id, 1L)) shouldBe Left(ServiceError.NotFound)
    }
  }
}
