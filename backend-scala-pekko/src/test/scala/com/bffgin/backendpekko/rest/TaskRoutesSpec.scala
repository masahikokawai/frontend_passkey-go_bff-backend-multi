package com.bffgin.backendpekko.rest

import com.bffgin.backendpekko._
import com.bffgin.backendpekko.JsonProtocol._
import com.bffgin.backendpekko.auth._
import org.apache.pekko.http.scaladsl.marshallers.sprayjson.SprayJsonSupport._
import org.apache.pekko.http.scaladsl.model.StatusCodes
import org.apache.pekko.http.scaladsl.model.headers.RawHeader
import org.apache.pekko.http.scaladsl.testkit.ScalatestRouteTest
import org.scalatest.matchers.should.Matchers
import org.scalatest.wordspec.AnyWordSpec

import com.nimbusds.jwt.SignedJWT

import java.time.LocalDate
import java.util.Base64

// backend/internal/handler/v1/task.go のステータスコード・エラーボディ契約(CONTRACT.mdセクション
// 5.1・20.5)がREST実装でも一致することを、実DB/実JWTを使わずにpekko-http-testkitで検証する
// ExecutionContextはScalatestRouteTestが提供するexecutorをそのまま使う(自前でimplicit valを
// 定義すると、こちらとRouteTest側のexecutorが曖昧になってコンパイルエラーになる)
class TaskRoutesSpec extends AnyWordSpec with Matchers with ScalatestRouteTest {

  private val fixedIssuer = "test-issuer"
  private val fixedSubject = "1"

  // JwtDispatcher.verifyは委譲先を決める前に`SignedJWT.parse`で3セグメントのJWS構造として
  // issクレームを覗く(署名検証はしない)ため、テスト用トークンも本物のJWTと同じ形式
  // (header.payload.signature、base64url)である必要がある
  // 【はまった点】alg:"none"にすると`SignedJWT.parse`自体が「これはunsecured JWTであり
  // SignedJWTではない」として例外を投げ、peekの時点で全トークンが一律に失敗扱いになってしまう
  // (署名検証はしていないのに、です)。alg自体はJWSとして認識される値であれば良く、実際の署名
  // バイト列はConditionalVerifierが検証しないため何でもよい
  private def fakeToken(subject: String): String = {
    def b64(s: String): String = Base64.getUrlEncoder.withoutPadding.encodeToString(s.getBytes("UTF-8"))
    val header = b64("""{"alg":"HS256","typ":"JWT"}""")
    val payload = b64(s"""{"iss":"$fixedIssuer","sub":"$subject"}""")
    val fakeSignature = Base64.getUrlEncoder.withoutPadding.encodeToString(Array[Byte](1, 2, 3, 4))
    s"$header.$payload.$fakeSignature"
  }

  // 実際にJWTのペイロードを読み、subject="invalid-signature"のときだけ失敗させるfake
  // (署名検証そのものはこのテストの対象外。TaskRoutesの認証フロー・エラーマッピングだけを見る)
  private class ConditionalVerifier extends TokenVerifier {
    override def verify(token: String): Either[String, Claims] = {
      val claimsSet = SignedJWT.parse(token).getJWTClaimsSet
      if (claimsSet.getSubject == "invalid-signature") Left("signature invalid")
      else Right(Claims(claimsSet.getIssuer, claimsSet.getSubject, Set("backend")))
    }
  }

  private val validToken = fakeToken(fixedSubject)
  private val invalidSignatureToken = fakeToken("invalid-signature")

  private def newRoutes(userExists: Boolean): TaskRoutes = {
    val userRepo = new InMemoryUserRepository
    // fixedIssuerはローカル発行issuer(bff-gin-local-hmac/rsa)ではないため、resolveUserIdは
    // getByKeycloakSubへ流れる。fixedSubject("1")をそのままkeycloak_subとして登録しておく
    if (userExists) userRepo.seed(UserDTO(id = 1, keycloakSub = fixedSubject, email = "a@example.com", name = "A", role = 1))
    val taskRepo = new InMemoryTaskRepository
    val service = new TaskService(taskRepo, userRepo)
    val dispatcher = new JwtDispatcher(Map(fixedIssuer -> new ConditionalVerifier))
    new TaskRoutes(service, dispatcher)
  }

  "GET /internal/v1/tasks" should {
    "Authorizationヘッダが無ければ401 unauthorized" in {
      val routes = newRoutes(userExists = true)
      Get("/internal/v1/tasks") ~> routes.routes ~> check {
        status shouldBe StatusCodes.Unauthorized
        responseAs[ErrorBody].error shouldBe "unauthorized"
      }
    }

    "不正なトークンなら401 invalid_token" in {
      val routes = newRoutes(userExists = true)
      Get("/internal/v1/tasks") ~> RawHeader("Authorization", "Bearer " + invalidSignatureToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.Unauthorized
        responseAs[ErrorBody].error shouldBe "invalid_token"
      }
    }

    "有効なトークンだがusersに未登録なら403 user_not_provisioned" in {
      val routes = newRoutes(userExists = false)
      Get("/internal/v1/tasks") ~> RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.Forbidden
        responseAs[ErrorBody].error shouldBe "user_not_provisioned"
      }
    }

    "不正なstatusクエリパラメータは422 invalid_status" in {
      val routes = newRoutes(userExists = true)
      Get("/internal/v1/tasks?status=bogus") ~> RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.UnprocessableEntity
        responseAs[ErrorBody].error shouldBe "invalid_status"
      }
    }

    "妥当なリクエストは200でtasks/total/limit/offsetを返す" in {
      val routes = newRoutes(userExists = true)
      Get("/internal/v1/tasks") ~> RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.OK
        val body = responseAs[TaskListResponse]
        body.tasks shouldBe empty
        body.total shouldBe 0
        body.limit shouldBe 20
        body.offset shouldBe 0
      }
    }
  }

  "POST /internal/v1/tasks" should {
    "必須項目(name)が無ければ400 invalid_request" in {
      val routes = newRoutes(userExists = true)
      Post("/internal/v1/tasks", TaskRequestBody(None, None, Some("waiting"), Some("2099-01-01"), None)) ~>
        RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
          status shouldBe StatusCodes.BadRequest
          responseAs[ErrorBody].error shouldBe "invalid_request"
        }
    }

    "finished_onの形式が不正なら422 invalid_finished_on" in {
      val routes = newRoutes(userExists = true)
      Post("/internal/v1/tasks", TaskRequestBody(Some("task"), None, Some("waiting"), Some("not-a-date"), None)) ~>
        RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
          status shouldBe StatusCodes.UnprocessableEntity
          responseAs[ErrorBody].error shouldBe "invalid_finished_on"
        }
    }

    "nameが21文字以上なら422 validation_error" in {
      val routes = newRoutes(userExists = true)
      val longName = "あ" * 21
      Post("/internal/v1/tasks", TaskRequestBody(Some(longName), None, Some("waiting"), Some("2099-01-01"), None)) ~>
        RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
          status shouldBe StatusCodes.UnprocessableEntity
          responseAs[ErrorBody].error shouldBe "validation_error"
        }
    }

    "妥当なリクエストは201でTaskを返す" in {
      val routes = newRoutes(userExists = true)
      Post("/internal/v1/tasks", TaskRequestBody(Some("task"), None, Some("waiting"), Some("2099-01-01"), None)) ~>
        RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
          status shouldBe StatusCodes.Created
          val task = responseAs[TaskDTO]
          task.name shouldBe "task"
          task.status shouldBe "waiting"
          task.finishedOn shouldBe LocalDate.parse("2099-01-01")
        }
    }
  }

  "GET/PATCH/DELETE /internal/v1/tasks/:id" should {
    "存在しないIDのGETは404 not_found" in {
      val routes = newRoutes(userExists = true)
      Get("/internal/v1/tasks/999") ~> RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.NotFound
        responseAs[ErrorBody].error shouldBe "not_found"
      }
    }

    "存在しないIDのDELETEは404 not_found" in {
      val routes = newRoutes(userExists = true)
      Delete("/internal/v1/tasks/999") ~> RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.NotFound
        responseAs[ErrorBody].error shouldBe "not_found"
      }
    }

    "作成したtaskはDELETEで204、以後のGETは404になる" in {
      val routes = newRoutes(userExists = true)
      var createdId: Long = -1
      Post("/internal/v1/tasks", TaskRequestBody(Some("task"), None, Some("waiting"), Some("2099-01-01"), None)) ~>
        RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
          createdId = responseAs[TaskDTO].id
        }
      Delete(s"/internal/v1/tasks/$createdId") ~> RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.NoContent
      }
      Get(s"/internal/v1/tasks/$createdId") ~> RawHeader("Authorization", "Bearer " + validToken) ~> routes.routes ~> check {
        status shouldBe StatusCodes.NotFound
      }
    }
  }
}
