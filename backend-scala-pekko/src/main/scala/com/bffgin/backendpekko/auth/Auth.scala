package com.bffgin.backendpekko.auth

import com.nimbusds.jose.JWSAlgorithm
import com.nimbusds.jose.crypto.MACVerifier
import com.nimbusds.jose.jwk.source.{JWKSource, RemoteJWKSet}
import com.nimbusds.jose.proc.{JWSVerificationKeySelector, SecurityContext}
import com.nimbusds.jwt.proc.{DefaultJWTClaimsVerifier, DefaultJWTProcessor}
import com.nimbusds.jwt.{JWTClaimsSet, SignedJWT}

import java.net.{URI, URL}
import java.util.Date
import scala.jdk.CollectionConverters._
import scala.util.Try

// backend/internal/authjwt/dispatcher.go の Claims に対応(このRust/Scala実装が使う分だけ)
// azpはCONTRACT.mdセクション11(外部公開API)のRequireExternalClientAuthが使うクレーム
// (Client Credentials Grantで発行されたトークンの発行先クライアントID)。ローカルHMAC/RSA
// 発行のトークンにはazpが無いためNoneのままでよい(外部公開APIはKeycloak発行トークンのみ対象)
final case class Claims(issuer: String, subject: String, audience: Set[String], azp: Option[String] = None)

// backend/internal/authjwt/dispatcher.go の LocalHMACIssuer/LocalRSAIssuer/IsLocalIssuer に対応
object JwtIssuers {
  val LocalHmac = "bff-gin-local-hmac"
  val LocalRsa = "bff-gin-local-rsa"
  def isLocal(issuer: String): Boolean = issuer == LocalHmac || issuer == LocalRsa
}

// backend/internal/authjwt/dispatcher.go の TokenVerifier に対応
trait TokenVerifier {
  def verify(token: String): Either[String, Claims]
}

// backend/internal/authjwt/jwks.go の *Verifier に対応(Keycloak/ローカルRSAどちらもこれで良い、
// どちらもRS256+JWKSの検証方式のため、jwksUrl/issuerが違うだけで実装は共通化できる)
// nimbus-jose-jwtのRemoteJWKSetがkidベースのキャッシュ・再取得をライブラリ側で担ってくれるため、
// Goのjwks.goのようにキャッシュを自前実装する必要はない
final class JwksVerifier(jwksUrl: String, issuer: String, audience: String) extends TokenVerifier {
  private val jwkSource: JWKSource[SecurityContext] = new RemoteJWKSet(URI.create(jwksUrl).toURL)
  private val processor = new DefaultJWTProcessor[SecurityContext]()
  processor.setJWSKeySelector(new JWSVerificationKeySelector(JWSAlgorithm.RS256, jwkSource))
  private val requiredClaims = new JWTClaimsSet.Builder().issuer(issuer).build()
  processor.setJWTClaimsSetVerifier(
    new DefaultJWTClaimsVerifier[SecurityContext](requiredClaims, java.util.Set.of("exp", "iat"))
  )

  override def verify(token: String): Either[String, Claims] =
    Try {
      val claimsSet = processor.process(token, null)
      val aud = Option(claimsSet.getAudience).map(_.asScala.toSet).getOrElse(Set.empty[String])
      if (!aud.contains(audience)) {
        throw new IllegalArgumentException(s"audience mismatch: expected=$audience actual=$aud")
      }
      val azp = Option(claimsSet.getStringClaim("azp"))
      Claims(claimsSet.getIssuer, claimsSet.getSubject, aud, azp)
    }.toEither.left.map(_.getMessage)
}

// backend/internal/authjwt/hmac.go の HMACVerifier に対応(ローカルHMAC版)
final class HmacVerifier(secret: String, issuer: String, audience: String) extends TokenVerifier {
  private val verifier = new MACVerifier(secret.getBytes("UTF-8"))

  override def verify(token: String): Either[String, Claims] =
    Try {
      val jwt = SignedJWT.parse(token)
      if (!jwt.verify(verifier)) {
        throw new IllegalArgumentException("signature invalid")
      }
      val claims = jwt.getJWTClaimsSet
      if (claims.getIssuer != issuer) {
        throw new IllegalArgumentException(s"issuer mismatch: expected=$issuer actual=${claims.getIssuer}")
      }
      val aud = Option(claims.getAudience).map(_.asScala.toSet).getOrElse(Set.empty[String])
      if (!aud.contains(audience)) {
        throw new IllegalArgumentException(s"audience mismatch: expected=$audience actual=$aud")
      }
      val exp = claims.getExpirationTime
      if (exp == null || exp.before(new Date())) {
        throw new IllegalArgumentException("token expired")
      }
      Claims(claims.getIssuer, claims.getSubject, aud)
    }.toEither.left.map(_.getMessage)
}

// backend/internal/authjwt/dispatcher.go の Dispatcher に対応
// issuerクレームを署名検証前に覗いて振り分け先を決め、実際の信頼は委譲先verifierの
// 署名検証に委ねる、という2段階構造も同じ(Goのコメント参照)
final class JwtDispatcher(byIssuer: Map[String, TokenVerifier]) {
  def verify(token: String): Either[String, Claims] =
    peekIssuer(token) match {
      case None => Left("JWTのパースに失敗(iss確認前)")
      case Some(issuer) =>
        byIssuer.get(issuer) match {
          case Some(v) => v.verify(token)
          case None    => Left(s"不明なissuer: $issuer")
        }
    }

  private def peekIssuer(token: String): Option[String] =
    Try(SignedJWT.parse(token).getJWTClaimsSet.getIssuer).toOption
}
