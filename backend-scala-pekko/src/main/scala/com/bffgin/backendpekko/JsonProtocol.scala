package com.bffgin.backendpekko

import spray.json._

import java.time.format.DateTimeFormatter
import java.time.{Instant, LocalDate}
import scala.util.{Failure, Success, Try}

// CONTRACT.mdセクション5.1のJSON形状(スネークケース)にそのまま合わせる必要があるため、
// spray-jsonのjsonFormatNマクロ(Scalaのフィールド名をそのままキーにする)は使わず、
// レスポンス用は手書きのRootJsonFormatにしている
// (backend/internal/handler/v1/task.go の taskDTOToJSON のコメントにある通り、
// camelCaseで返すとbffのtaskV1DTOがfinished_on/created_at/updated_atを空文字のまま読んでしまう)
object JsonProtocol extends DefaultJsonProtocol {

  private val instantFmt = DateTimeFormatter.ISO_INSTANT

  implicit object LabelDTOFormat extends RootJsonFormat[LabelDTO] {
    def write(l: LabelDTO): JsValue = JsObject("id" -> JsNumber(l.id), "name" -> JsString(l.name))
    def read(json: JsValue): LabelDTO = json.asJsObject.getFields("id", "name") match {
      case Seq(JsNumber(id), JsString(name)) => LabelDTO(id.toLong, name)
      case _                                  => deserializationError("LabelDTOの形式が不正")
    }
  }

  implicit object TaskDTOFormat extends RootJsonFormat[TaskDTO] {
    def write(t: TaskDTO): JsValue = JsObject(
      "id" -> JsNumber(t.id),
      "name" -> JsString(t.name),
      "description" -> t.description.map(JsString(_): JsValue).getOrElse(JsNull),
      "status" -> JsString(t.status),
      "finished_on" -> JsString(t.finishedOn.toString),
      "user_id" -> JsNumber(t.userId),
      "labels" -> JsArray(t.labels.map(_.toJson).toVector),
      "created_at" -> JsString(instantFmt.format(t.createdAt)),
      "updated_at" -> JsString(instantFmt.format(t.updatedAt))
    )
    // backendはこのレスポンスを自分でパースし直す側にはならない(リクエストボディは
    // TaskRequestBodyが別に担う)が、テストコード(TaskRoutesSpec)がresponseAsで検証できるよう
    // readも実装しておく
    def read(json: JsValue): TaskDTO = {
      val fields = json.asJsObject.fields
      def str(key: String): String = fields(key) match {
        case JsString(s) => s
        case other        => deserializationError(s"$key はstringのはず: $other")
      }
      TaskDTO(
        id = fields("id").convertTo[Long],
        name = str("name"),
        description = fields.get("description").collect { case JsString(s) => s },
        status = str("status"),
        finishedOn = LocalDate.parse(str("finished_on")),
        userId = fields("user_id").convertTo[Long],
        labels = fields("labels").convertTo[Seq[LabelDTO]],
        createdAt = Instant.parse(str("created_at")),
        updatedAt = Instant.parse(str("updated_at"))
      )
    }
  }

  final case class TaskListResponse(tasks: Seq[TaskDTO], total: Int, limit: Int, offset: Int)
  implicit val taskListResponseFormat: RootJsonFormat[TaskListResponse] = jsonFormat4(TaskListResponse.apply)

  final case class ErrorBody(error: String, message: Option[String] = None)
  implicit val errorBodyFormat: RootJsonFormat[ErrorBody] = jsonFormat2(ErrorBody.apply)

  // POST/PATCH共通のリクエストボディ。CONTRACT.mdセクション5.1のキー名(name/description/status/
  // finished_on/label_ids)にそのまま合わせるため、あえてsnake_caseのScalaフィールド名にしている
  // (backend/internal/handler/v1/task.go の taskRequestBody と同じ理由でのトレードオフ)
  final case class TaskRequestBody(
      name: Option[String],
      description: Option[String],
      status: Option[String],
      finished_on: Option[String],
      label_ids: Option[Seq[Long]]
  )
  implicit val taskRequestBodyFormat: RootJsonFormat[TaskRequestBody] = jsonFormat5(TaskRequestBody.apply)

  // TaskRequestBody(生の文字列)→TaskInput(型付き)への変換。Go実装の型付けの区別
  //   - 必須項目欠落/空文字 → 400 invalid_request(handler層)
  //   - finished_onのパース失敗 → 422 invalid_finished_on(handler層)
  //   - name長すぎ・過去日・status不正 → 422 validation_error(service層)
  // という3段階のエラー種別をそのまま踏襲するため、ここではパースのみ行い、業務バリデーションは
  // TaskService.validateに委ねる
  sealed trait ParseError
  object ParseError {
    case object InvalidRequest extends ParseError
    case object InvalidFinishedOn extends ParseError
  }

  def parseTaskInput(body: TaskRequestBody): Either[ParseError, TaskInput] =
    (body.name, body.status, body.finished_on) match {
      case (Some(n), Some(s), Some(f)) if n.nonEmpty && s.nonEmpty && f.nonEmpty =>
        Try(LocalDate.parse(f)) match {
          case Success(date) => Right(TaskInput(n, body.description, s, date, body.label_ids.getOrElse(Seq.empty)))
          case Failure(_)    => Left(ParseError.InvalidFinishedOn)
        }
      case _ => Left(ParseError.InvalidRequest)
    }
}
