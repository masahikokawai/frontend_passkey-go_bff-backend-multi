package com.bffgin.backend.auth

import cats.effect.{IO, Ref}
import cats.syntax.all._
import com.nimbusds.jwt.{JWTClaimsSet, SignedJWT}
import com.nimbusds.jose.JWSAlgorithm
import com.nimbusds.jose.crypto.{MACVerifier, RSASSAVerifier}
import com.nimbusds.jose.jwk.JWKSet
import com.bffgin.backend.{AppError, Config}
import java.net.URI
import java.time.Instant
import scala.jdk.CollectionConverters._

// azp(authorized party)は外部公開API(CONTRACT.mdセクション11)のRequireExternalClientAuth相当の
// チェックでのみ使う。Keycloak発行のClient Credentials Grantトークンにのみ意味のある値
final case class Claims(subject: String, issuer: String, azp: Option[String] = None)

/** backend/internal/authjwt/jwks.go の Verifier を再現する
  * kidごとにRSA公開鍵をキャッシュし、未知のkidが来たときだけJWKSを再取得する
  */
class JwksCache(url: String) {
  private val ref = Ref.unsafe[IO, Map[String, java.security.interfaces.RSAPublicKey]](Map.empty)

  def keyFor(kid: String): IO[Option[java.security.interfaces.RSAPublicKey]] =
    ref.get.map(_.get(kid)).flatMap {
      case some @ Some(_) => IO.pure(some)
      case None           => refresh() >> ref.get.map(_.get(kid))
    }

  private def refresh(): IO[Unit] =
    IO.blocking {
      val set = JWKSet.load(URI.create(url).toURL)
      set.getKeys.asScala.toList
        .filter(k => k.getKeyType.getValue == "RSA")
        .flatMap { k =>
          scala.util.Try(k.toRSAKey.toRSAPublicKey).toOption.map(pk => k.getKeyID -> pk)
        }
        .toMap
    }.flatMap(ref.set)
}

/** backend/internal/authjwt/dispatcher.go の Dispatcher を再現する
  * issuerで3方式(Keycloak JWKS / ローカルHMAC / ローカルRSA JWKS)を振り分ける
  */
class JwtAuth(cfg: Config) {
  private val keycloakJwks = new JwksCache(cfg.keycloakJwksUrl)
  private val localRsaJwks = new JwksCache(cfg.localRsaJwksUrl)

  def isLocalIssuer(iss: String): Boolean =
    iss == cfg.localHmacIssuer || iss == cfg.localRsaIssuer

  /** Authorizationヘッダから切り出した生JWT文字列を検証する。失敗時は必ずAppError.InvalidTokenを投げる
    * (ヘッダ自体が無い/Bearer形式でないケースは呼び出し側でAppError.Unauthorizedにする)
    */
  def verify(tokenString: String): IO[Claims] =
    IO.fromEither(parseIssuer(tokenString))
      .flatMap { iss =>
        if (iss == cfg.localHmacIssuer) verifyHmac(tokenString)
        else if (iss == cfg.localRsaIssuer) verifyRsa(tokenString, localRsaJwks, cfg.localRsaIssuer)
        else if (iss == cfg.keycloakIssuer) verifyRsa(tokenString, keycloakJwks, cfg.keycloakIssuer)
        else IO.raiseError(AppError.InvalidToken)
      }
      .handleErrorWith {
        case e: AppError => IO.raiseError(e)
        case _            => IO.raiseError(AppError.InvalidToken)
      }

  private def parseIssuer(tokenString: String): Either[Throwable, String] =
    Either
      .catchNonFatal(SignedJWT.parse(tokenString).getJWTClaimsSet.getIssuer)
      .flatMap(Option(_).toRight(new RuntimeException("issクレームが無い")))

  private def validateCommonClaims(claims: JWTClaimsSet, expectedIssuer: String): IO[Unit] = {
    val now = Instant.now()
    val issOk = claims.getIssuer == expectedIssuer
    val audOk = Option(claims.getAudience).exists(_.asScala.contains(cfg.expectedAudience))
    val expOk = Option(claims.getExpirationTime).exists(_.toInstant.isAfter(now))
    val nbfOk = Option(claims.getNotBeforeTime).forall(_.toInstant.isBefore(now))
    if (issOk && audOk && expOk && nbfOk) IO.unit else IO.raiseError(AppError.InvalidToken)
  }

  private def verifyHmac(tokenString: String): IO[Claims] =
    for {
      jwt <- IO.fromEither(Either.catchNonFatal(SignedJWT.parse(tokenString)))
      _ <- IO.fromEither(
        Either.cond(jwt.getHeader.getAlgorithm == JWSAlgorithm.HS256, (), AppError.InvalidToken)
      )
      verifier = new MACVerifier(cfg.localHmacSecret.getBytes("UTF-8"))
      ok <- IO.blocking(jwt.verify(verifier))
      _  <- if (ok) IO.unit else IO.raiseError(AppError.InvalidToken)
      claims = jwt.getJWTClaimsSet
      _ <- validateCommonClaims(claims, cfg.localHmacIssuer)
    } yield Claims(subject = claims.getSubject, issuer = claims.getIssuer, azp = Option(claims.getStringClaim("azp")))

  private def verifyRsa(tokenString: String, jwks: JwksCache, expectedIssuer: String): IO[Claims] =
    for {
      jwt <- IO.fromEither(Either.catchNonFatal(SignedJWT.parse(tokenString)))
      _ <- IO.fromEither(
        Either.cond(jwt.getHeader.getAlgorithm == JWSAlgorithm.RS256, (), AppError.InvalidToken)
      )
      kid <- IO.fromOption(Option(jwt.getHeader.getKeyID))(AppError.InvalidToken)
      keyOpt <- jwks.keyFor(kid)
      key <- IO.fromOption(keyOpt)(AppError.InvalidToken)
      ok  <- IO.blocking(jwt.verify(new RSASSAVerifier(key)))
      _   <- if (ok) IO.unit else IO.raiseError(AppError.InvalidToken)
      claims = jwt.getJWTClaimsSet
      _ <- validateCommonClaims(claims, expectedIssuer)
    } yield Claims(subject = claims.getSubject, issuer = claims.getIssuer, azp = Option(claims.getStringClaim("azp")))
}
