package com.bffgin.backendpekko.external

import com.bffgin.backendpekko._
import com.bffgin.backendpekko.JsonProtocol.{ErrorBody, errorBodyFormat}
import com.bffgin.backendpekko.ExternalJsonProtocol._
import com.bffgin.backendpekko.auth._
import org.apache.pekko.http.scaladsl.marshallers.sprayjson.SprayJsonSupport._
import org.apache.pekko.http.scaladsl.model.StatusCodes
import org.apache.pekko.http.scaladsl.model.headers.RawHeader
import org.apache.pekko.http.scaladsl.testkit.ScalatestRouteTest
import org.scalatest.matchers.should.Matchers
import org.scalatest.wordspec.AnyWordSpec

import java.time.{Instant, LocalDate}
import java.util.Base64

// backend/internal/handler/external/task.go(GET /external/v1/tasks)のステータスコード・
// エラーボディ契約(CONTRACT.mdセクション11・20.7)を、実DB/実Keycloakを使わずに検証する
class ExternalTaskRoutesSpec extends AnyWordSpec with Matchers with ScalatestRouteTest {

  private val fixedIssuer = "test-keycloak-issuer"
  private val expectedClientId = "external-api-client"

  private def fakeToken(azp: Option[String]): String = {
    def b64(s: String): String = Base64.getUrlEncoder.withoutPadding.encodeToString(s.getBytes("UTF-8"))
    val header = b64("""{"alg":"HS256","typ":"JWT"}""")
    val azpField = azp.map(a => s""","azp":"$a"""").getOrElse("")
    val payload = b64(s"""{"iss":"$fixedIssuer","sub":"client-sub"$azpField}""")
    val fakeSignature = Base64.getUrlEncoder.withoutPadding.encodeToString(Array[Byte](1, 2, 3, 4))
    s"$header.$payload.$fakeSignature"
  }

  // TaskRoutesSpecと同じ理由(alg:"none"は使えない)でHS256ヘッダの生JWT形式を使う。
  // 署名検証自体はこのVerifierの対象外(azpクレームの伝播だけを見る)
  private class FakeExternalVerifier extends TokenVerifier {
    override def verify(token: String): Either[String, Claims] = {
      import com.nimbusds.jwt.SignedJWT
      val claimsSet = SignedJWT.parse(token).getJWTClaimsSet
      if (claimsSet.getSubject == "invalid-signature") Left("signature invalid")
      else Right(Claims(claimsSet.getIssuer, claimsSet.getSubject, Set("backend"), Option(claimsSet.getStringClaim("azp"))))
    }
  }

  private val validClientToken = fakeToken(Some(expectedClientId))
  private val wrongClientToken = fakeToken(Some("some-other-client"))
  private val noAzpToken = fakeToken(None)

  private class FixedFlagSource(value: Boolean) extends FlagSource {
    override def currentValue: Boolean = value
  }

  private def newRoutes(flagValue: Boolean): ExternalTaskRoutes = {
    val taskRepo = new InMemoryTaskRepository
    val userRepo = new InMemoryUserRepository
    val service = new TaskService(taskRepo, userRepo)
    val dispatcher = new JwtDispatcher(Map(fixedIssuer -> new FakeExternalVerifier))
    new ExternalTaskRoutes(service, dispatcher, expectedClientId, new FixedFlagSource(flagValue))
  }

  "GET /external/v1/tasks" should {
    "Authorizationヘッダが無ければ401 unauthorized" in {
      val routes = newRoutes(flagValue = false)
      Get("/external/v1/tasks?user_id=1") ~> routes.routes ~> check {
        status shouldBe StatusCodes.Unauthorized
        responseAs[ErrorBody].error shouldBe "unauthorized"
      }
    }

    "検証に失敗するトークンなら401 invalid_token" in {
      val routes = newRoutes(flagValue = false)
      val badToken = fakeToken(Some(expectedClientId)).split("\\.").toList match {
        case Seq(h, _, s) =>
          val badPayload = Base64.getUrlEncoder.withoutPadding.encodeToString(
            s"""{"iss":"$fixedIssuer","sub":"invalid-signature","azp":"$expectedClientId"}""".getBytes("UTF-8")
          )
          s"$h.$badPayload.$s"
        case _ => fail("unexpected token shape")
      }
      Get("/external/v1/tasks?user_id=1") ~> RawHeader("Authorization", "Bearer " + badToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.Unauthorized
        responseAs[ErrorBody].error shouldBe "invalid_token"
      }
    }

    "azpクレームが期待値と不一致なら403 client_not_allowed" in {
      val routes = newRoutes(flagValue = false)
      Get("/external/v1/tasks?user_id=1") ~> RawHeader("Authorization", "Bearer " + wrongClientToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.Forbidden
        responseAs[ErrorBody].error shouldBe "client_not_allowed"
      }
    }

    "azpクレームが無ければ403 client_not_allowed" in {
      val routes = newRoutes(flagValue = false)
      Get("/external/v1/tasks?user_id=1") ~> RawHeader("Authorization", "Bearer " + noAzpToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.Forbidden
        responseAs[ErrorBody].error shouldBe "client_not_allowed"
      }
    }

    "user_idクエリが無ければ400" in {
      val routes = newRoutes(flagValue = false)
      Get("/external/v1/tasks") ~> RawHeader("Authorization", "Bearer " + validClientToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.BadRequest
        responseAs[ErrorBody].error shouldBe "user_id is required"
      }
    }

    "user_idが数値でなければ400" in {
      val routes = newRoutes(flagValue = false)
      Get("/external/v1/tasks?user_id=abc") ~> RawHeader("Authorization", "Bearer " + validClientToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.BadRequest
        responseAs[ErrorBody].error shouldBe "invalid user_id"
      }
    }

    "flag OFF(既定)ならoffsetページング形状(page/page_size/total)で返す" in {
      val routes = newRoutes(flagValue = false)
      Get("/external/v1/tasks?user_id=1") ~> RawHeader("Authorization", "Bearer " + validClientToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.OK
        val body = responseAs[ExternalListV1Response]
        body.page shouldBe 1
        body.page_size shouldBe 10
        body.total shouldBe 0
        body.tasks shouldBe empty
      }
    }

    "flag ONならcursorページング形状(next_cursor/limit)で返す" in {
      val routes = newRoutes(flagValue = true)
      Get("/external/v1/tasks?user_id=1") ~> RawHeader("Authorization", "Bearer " + validClientToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.OK
        val body = responseAs[ExternalListV2Response]
        body.limit shouldBe 10
        body.next_cursor shouldBe None
        body.tasks shouldBe empty
      }
    }

    "不正なcursorは422" in {
      val routes = newRoutes(flagValue = true)
      Get("/external/v1/tasks?user_id=1&cursor=not-valid-base64!!!") ~> RawHeader(
        "Authorization",
        "Bearer " + validClientToken
      ) ~> routes.routes ~> check {
        status shouldBe StatusCodes.UnprocessableEntity
        responseAs[ErrorBody].error shouldBe "invalid_cursor"
      }
    }

    "レスポンスのTask JSONにはuser_idが含まれない" in {
      val taskRepo = new InMemoryTaskRepository
      taskRepo.seed(
        TaskDTO(1, "t1", None, "waiting", LocalDate.parse("2030-01-01"), userId = 1, Seq.empty, Instant.now(), Instant.now())
      )
      val userRepo = new InMemoryUserRepository
      val service = new TaskService(taskRepo, userRepo)
      val dispatcher = new JwtDispatcher(Map(fixedIssuer -> new FakeExternalVerifier))
      val routes = new ExternalTaskRoutes(service, dispatcher, expectedClientId, new FixedFlagSource(false))

      Get("/external/v1/tasks?user_id=1") ~> RawHeader("Authorization", "Bearer " + validClientToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.OK
        val raw = responseAs[String]
        raw should not include "\"user_id\""
        raw should include("\"t1\"")
      }
    }
  }
}
