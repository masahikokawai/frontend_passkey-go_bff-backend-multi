package com.bffgin.backend.auth

import cats.effect.IO
import munit.CatsEffectSuite
import com.bffgin.backend.{AppError, Config}
import com.nimbusds.jose.{JWSAlgorithm, JWSHeader}
import com.nimbusds.jose.crypto.MACSigner
import com.nimbusds.jwt.{JWTClaimsSet, SignedJWT}
import java.util.Date

// backend/internal/authjwt/hmac.go・dispatcher.go の検証ロジックをそのまま再現していることを確認する
// (ローカルHMAC発行トークンの検証。JWKSネットワークアクセスが不要なため、このスイートだけで完結する)
class JwtAuthSuite extends CatsEffectSuite {
  private val cfg  = Config.load()
  private val auth = new JwtAuth(cfg)

  private def signHmac(
      subject: String = "1",
      issuer: String = cfg.localHmacIssuer,
      audience: String = cfg.expectedAudience,
      secret: String = cfg.localHmacSecret,
      expiresInSeconds: Long = 3600,
      notBeforeOffsetSeconds: Long = 0
  ): String = {
    val now = new Date()
    val claims = new JWTClaimsSet.Builder()
      .subject(subject)
      .issuer(issuer)
      .audience(audience)
      .issueTime(now)
      .notBeforeTime(new Date(now.getTime + notBeforeOffsetSeconds * 1000))
      .expirationTime(new Date(now.getTime + expiresInSeconds * 1000))
      .build()
    val jwt = new SignedJWT(new JWSHeader(JWSAlgorithm.HS256), claims)
    jwt.sign(new MACSigner(secret.getBytes("UTF-8")))
    jwt.serialize()
  }

  test("正しい秘密鍵・issuer・audienceで署名されたHMACトークンは検証を通り、subject/issuerを返す") {
    val token = signHmac()
    auth.verify(token).map { claims =>
      assertEquals(claims.subject, "1")
      assertEquals(claims.issuer, cfg.localHmacIssuer)
    }
  }

  test("違う秘密鍵で署名されたトークンはInvalidToken") {
    val token = signHmac(secret = "wrong-secret-wrong-secret-wrong-long-enough-for-hs256")
    auth.verify(token).attempt.map(result => assertEquals(result, Left(AppError.InvalidToken)))
  }

  test("audienceが違うトークンはInvalidToken") {
    val token = signHmac(audience = "not-backend")
    auth.verify(token).attempt.map(result => assertEquals(result, Left(AppError.InvalidToken)))
  }

  test("期限切れのトークンはInvalidToken") {
    val token = signHmac(expiresInSeconds = -10)
    auth.verify(token).attempt.map(result => assertEquals(result, Left(AppError.InvalidToken)))
  }

  test("nbfが未来のトークンはInvalidToken") {
    val token = signHmac(notBeforeOffsetSeconds = 600)
    auth.verify(token).attempt.map(result => assertEquals(result, Left(AppError.InvalidToken)))
  }

  test("不明なissuerのトークンはInvalidToken(どのverifierにも振り分けられない)") {
    val token = signHmac(issuer = "unknown-issuer")
    auth.verify(token).attempt.map(result => assertEquals(result, Left(AppError.InvalidToken)))
  }

  test("isLocalIssuer: ローカルHMAC/RSA issuerはtrue、Keycloak issuerはfalse") {
    assert(auth.isLocalIssuer(cfg.localHmacIssuer))
    assert(auth.isLocalIssuer(cfg.localRsaIssuer))
    assert(!auth.isLocalIssuer(cfg.keycloakIssuer))
  }
}
