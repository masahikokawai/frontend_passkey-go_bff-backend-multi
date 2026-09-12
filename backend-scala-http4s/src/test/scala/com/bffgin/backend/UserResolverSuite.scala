package com.bffgin.backend

import cats.effect.IO
import munit.CatsEffectSuite
import com.bffgin.backend.auth.{Claims, JwtAuth}

class FakeUserLookup(byId: Map[Long, User] = Map.empty, byKeycloakSub: Map[String, User] = Map.empty) extends UserLookup {
  def findUserById(id: Long): IO[Option[User]]              = IO.pure(byId.get(id))
  def findUserByKeycloakSub(sub: String): IO[Option[User]] = IO.pure(byKeycloakSub.get(sub))
}

// backend/internal/handler/v1/task.go・grpcserver/task_service.go の resolveUserID を再現する
// UserResolverのテスト。ローカル発行issuer/Keycloak発行issuerそれぞれでのユーザー解決、
// 未プロビジョニング時にAppError.UserNotProvisionedを投げることを確認する
class UserResolverSuite extends CatsEffectSuite {
  private val cfg = Config.load()
  private val auth = new JwtAuth(cfg)

  test("ローカル発行(HMAC)issuer: subが内部user_idそのもの、users.idで見つかればそのIDを返す") {
    val lookup = new FakeUserLookup(byId = Map(42L -> User(42L, "a@example.com", "A", 1)))
    val claims = Claims(subject = "42", issuer = cfg.localHmacIssuer)
    UserResolver.resolve(claims, lookup, auth).map(id => assertEquals(id, 42L))
  }

  test("ローカル発行(RSA)issuer: subが内部user_idそのもの") {
    val lookup = new FakeUserLookup(byId = Map(7L -> User(7L, "b@example.com", "B", 1)))
    val claims = Claims(subject = "7", issuer = cfg.localRsaIssuer)
    UserResolver.resolve(claims, lookup, auth).map(id => assertEquals(id, 7L))
  }

  test("ローカル発行issuerでsubが数値でない場合はUserNotProvisioned") {
    val lookup = new FakeUserLookup()
    val claims = Claims(subject = "not-a-number", issuer = cfg.localHmacIssuer)
    UserResolver.resolve(claims, lookup, auth).attempt.map { result =>
      assertEquals(result, Left(AppError.UserNotProvisioned))
    }
  }

  test("ローカル発行issuerでusersに該当行が無い場合はUserNotProvisioned") {
    val lookup = new FakeUserLookup(byId = Map.empty)
    val claims = Claims(subject = "999", issuer = cfg.localHmacIssuer)
    UserResolver.resolve(claims, lookup, auth).attempt.map { result =>
      assertEquals(result, Left(AppError.UserNotProvisioned))
    }
  }

  test("Keycloak発行issuer: subはkeycloak_subとして検索される") {
    val lookup = new FakeUserLookup(byKeycloakSub = Map("sub-abc" -> User(3L, "c@example.com", "C", 1)))
    val claims = Claims(subject = "sub-abc", issuer = cfg.keycloakIssuer)
    UserResolver.resolve(claims, lookup, auth).map(id => assertEquals(id, 3L))
  }

  test("Keycloak発行issuerでkeycloak_subが未登録の場合はUserNotProvisioned") {
    val lookup = new FakeUserLookup()
    val claims = Claims(subject = "unknown-sub", issuer = cfg.keycloakIssuer)
    UserResolver.resolve(claims, lookup, auth).attempt.map { result =>
      assertEquals(result, Left(AppError.UserNotProvisioned))
    }
  }

  // 【セキュリティ監査で追加】UserResolver.resolveはClaims.azpを一切参照していない。
  // そのため外部公開API用のClient Credentials Grantトークン(external-api-clientが取得する、
  // aud=backendが注入されたトークン)は、署名検証・issuer検証等の「認証」自体は
  // 内部API(TaskRoutes等)も通過してしまう。これが安全なのは、external-api-client自身の
  // サービスアカウントsub(Keycloakの慣例で"service-account-external-api-client"のような
  // 形式になる)が、JITプロビジョニング(通常ユーザーのログイン時のみ実行される)を
  // 一度も経ておらずusersテーブルに該当行が存在しないため、上のテストと同じ
  // 「Keycloak発行issuerでkeycloak_subが未登録→UserNotProvisioned」という経路で
  // 必ず拒否されるという「2段構えの安全性」に依存しているからである。
  // このテストは、その2段構えの安全性を明示的に固定し、将来UserResolverの実装が
  // 変わってこの前提が崩れた場合に検知できるようにする(Go実装backend/・Rust実装
  // backend-rust/で同じ観点のテストが既に存在し、4言語比較実装すべてに追加する一環)。
  // azpに実際の値("external-api-client")を設定しても判定に一切影響しない
  // (subだけで拒否される)ことも合わせて確認する。
  test("外部公開API用トークン(external-api-clientのサービスアカウント)は内部APIのuser解決に失敗する") {
    val lookup = new FakeUserLookup() // usersテーブルには何も登録しない(JIT未実行を再現)
    val claims = Claims(
      subject = "service-account-external-api-client",
      issuer = cfg.keycloakIssuer,
      azp = Some("external-api-client")
    )
    UserResolver.resolve(claims, lookup, auth).attempt.map { result =>
      assertEquals(result, Left(AppError.UserNotProvisioned))
    }
  }
}
