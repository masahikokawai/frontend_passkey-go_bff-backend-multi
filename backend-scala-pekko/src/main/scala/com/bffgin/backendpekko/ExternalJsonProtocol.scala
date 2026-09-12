package com.bffgin.backendpekko

import spray.json._

import java.time.format.DateTimeFormatter

// CONTRACT.mdセクション11: 外部公開APIのTask JSON形状は内部CRUDと違い user_id を含まない
// (backend/internal/handler/external/task.go の taskDTOToJSON を参照。意図的な差異)
object ExternalJsonProtocol extends DefaultJsonProtocol {
  private val instantFmt = DateTimeFormatter.ISO_INSTANT

  implicit object ExternalLabelDTOFormat extends RootJsonFormat[LabelDTO] {
    def write(l: LabelDTO): JsValue = JsObject("id" -> JsNumber(l.id), "name" -> JsString(l.name))
    def read(json: JsValue): LabelDTO = deserializationError("読み取りは未対応")
  }

  implicit object ExternalTaskDTOFormat extends RootJsonFormat[TaskDTO] {
    def write(t: TaskDTO): JsValue = JsObject(
      "id" -> JsNumber(t.id),
      "name" -> JsString(t.name),
      "description" -> t.description.map(JsString(_): JsValue).getOrElse(JsNull),
      "status" -> JsString(t.status),
      "finished_on" -> JsString(t.finishedOn.toString),
      "labels" -> JsArray(t.labels.map(_.toJson(ExternalLabelDTOFormat)).toVector),
      "created_at" -> JsString(instantFmt.format(t.createdAt)),
      "updated_at" -> JsString(instantFmt.format(t.updatedAt))
    )
    def read(json: JsValue): TaskDTO = deserializationError("読み取りは未対応")
  }

  final case class ExternalListV1Response(tasks: Seq[TaskDTO], page: Int, page_size: Int, total: Int)
  implicit val externalListV1ResponseFormat: RootJsonFormat[ExternalListV1Response] =
    jsonFormat4(ExternalListV1Response.apply)

  final case class ExternalListV2Response(tasks: Seq[TaskDTO], next_cursor: Option[String], limit: Int)
  implicit val externalListV2ResponseFormat: RootJsonFormat[ExternalListV2Response] =
    jsonFormat3(ExternalListV2Response.apply)
}
