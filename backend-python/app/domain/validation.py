from __future__ import annotations

from datetime import date

from app.domain.errors import TaskError
from app.domain.models import TaskInput, TaskStatus

MAX_NAME_CODEPOINTS = 20


def validate_task_input(input_: TaskInput, today: date) -> TaskStatus:
    """backend(Go)のservice.validateTaskInputと同じルール:
        - name: 必須・20コードポイント以内
        - finished_on: 過去日不可(UTC基準の「今日」)
        - status: enumの範囲内

    このプロジェクトは全言語で一貫して「ビジネスルールの検証を自前実装する」方針を貫いている
    (Pydanticのバリデータには任せない)。Pydanticは構造的な型変換(JSON文字列 -> TaskInput、
    必須フィールドの有無)にのみ使い、ここでのビジネスルール検証は他言語のTaskValidation
    相当と同じ、明示的な関数として書く(README.md「アーキテクチャ選定」節参照)。

    【Python固有の利点、他言語との対比】nameの文字数判定は「コードポイント数」で行う必要が
    あり、Java/Kotlin/JavaScriptのString#lengthはUTF-16コード単位数を返すため、基本多言語面外
    の文字(絵文字等、サロゲートペア)を含む名前ではサロゲートペア1文字が2としてカウントされ、
    契約違反になる既知の落とし穴がある(backend-scala-http4s/backend-java/backend-kotlinで
    実際に見つかった不一致)。Python 3の`str`はPEP 393のフレキシブル文字列表現により、内部的に
    常にコードポイント単位で格納されるため、組み込みの`len()`が最初から正しくコードポイント数を
    返す。つまりPythonにはこの種の落とし穴自体が存在しない(自前でcodePointCount相当の関数を
    呼ぶ必要が無い、というのがJava/Kotlin/JSとの対比で興味深い点)
    """
    if not input_.name:
        raise TaskError.validation("nameは必須です")
    if len(input_.name) > MAX_NAME_CODEPOINTS:
        raise TaskError.validation("nameは20文字以内である必要があります")

    if input_.finished_on is None or input_.finished_on < today:
        raise TaskError.validation("finished_onに過去日は指定できません")

    status = TaskStatus.from_wire_value(input_.status_raw)
    if status is None:
        raise TaskError.validation(f'不明なstatus: "{input_.status_raw}"')
    return status


def parse_finished_on(raw: str) -> date:
    """カレンダー上の妥当性まで検証する(例: "2026-02-30"は文字列としては整形式だが実在しない日付)。
    `date.fromisoformat`はこれを`ValueError`で拒否するため、`invalid_finished_on`にマッピングする
    (backend-kotlinのTaskRoutes.kt/TaskProtoMapper.ktの`LocalDate.parse`と同じ役割の分離:
    「形式として壊れている」はinvalid_finished_on、「形式は正しいが過去日」はvalidation_error)
    """
    try:
        return date.fromisoformat(raw)
    except ValueError as exc:
        raise TaskError.invalid_finished_on() from exc
