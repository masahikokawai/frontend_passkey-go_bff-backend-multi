use chrono::{NaiveDate, NaiveDateTime};

/// backend(Go)の internal/model/enum.go の TaskStatus (waiting=1, work_in_progress=2, completed=3) と
/// 完全に同じマッピングを再現する
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TaskStatus {
    Waiting = 1,
    WorkInProgress = 2,
    Completed = 3,
}

impl TaskStatus {
    pub fn as_str(&self) -> &'static str {
        match self {
            TaskStatus::Waiting => "waiting",
            TaskStatus::WorkInProgress => "work_in_progress",
            TaskStatus::Completed => "completed",
        }
    }

    pub fn from_str(s: &str) -> Option<Self> {
        match s {
            "waiting" => Some(TaskStatus::Waiting),
            "work_in_progress" => Some(TaskStatus::WorkInProgress),
            "completed" => Some(TaskStatus::Completed),
            _ => None,
        }
    }

    pub fn from_db(v: u8) -> Option<Self> {
        match v {
            1 => Some(TaskStatus::Waiting),
            2 => Some(TaskStatus::WorkInProgress),
            3 => Some(TaskStatus::Completed),
            _ => None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct Label {
    pub id: u64,
    pub name: String,
}

#[derive(Debug, Clone)]
pub struct Task {
    pub id: u64,
    pub name: String,
    pub description: Option<String>,
    pub status: TaskStatus,
    pub finished_on: NaiveDate,
    pub user_id: u64,
    pub created_at: NaiveDateTime,
    pub updated_at: NaiveDateTime,
    pub labels: Vec<Label>,
}

// keycloak_subはusersテーブルには無く、別テーブルuser_keycloaksにある
// (db::find_user_by_keycloak_subのJOIN参照、backend(Go)のmigration 000008)
#[derive(Debug, Clone)]
pub struct User {
    pub id: u64,
    pub email: String,
    pub name: String,
}

/// Create/Updateの入力。backend(Go)の service.TaskInput 相当
#[derive(Debug, Clone)]
pub struct TaskInput {
    pub name: String,
    pub description: Option<String>,
    pub status: String,
    pub finished_on: NaiveDate,
    pub label_ids: Vec<u64>,
}

/// backend(Go)の service.validateTaskInput と同じルール:
///   - name: 必須・20文字以内
///   - finished_on: 過去日不可
///   - status: enumの範囲内
/// 戻り値のErrはHTTP 422 {"error":"validation_error","message":"..."} /
/// gRPC codes::InvalidArgument に変換される(REST側はハンドラでinvalid_finished_on/invalid_statusに
/// 分岐する箇所が別途あるため、ここではservice層のバリデーションだけを担う)
pub fn validate_task_input(input: &TaskInput, today: NaiveDate) -> Result<TaskStatus, String> {
    if input.name.is_empty() {
        return Err("nameは必須です".to_string());
    }
    if input.name.chars().count() > 20 {
        return Err("nameは20文字以内である必要があります".to_string());
    }
    if input.finished_on < today {
        return Err("finished_onに過去日は指定できません".to_string());
    }
    TaskStatus::from_str(&input.status).ok_or_else(|| format!("不明なstatus: \"{}\"", input.status))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn valid_input(today: NaiveDate) -> TaskInput {
        TaskInput {
            name: "buy milk".to_string(),
            description: None,
            status: "waiting".to_string(),
            finished_on: today,
            label_ids: vec![],
        }
    }

    #[test]
    fn validate_task_input_accepts_valid_input() {
        let today = NaiveDate::from_ymd_opt(2026, 9, 9).unwrap();
        let got = validate_task_input(&valid_input(today), today);
        assert_eq!(got, Ok(TaskStatus::Waiting));
    }

    #[test]
    fn validate_task_input_rejects_empty_name() {
        let today = NaiveDate::from_ymd_opt(2026, 9, 9).unwrap();
        let mut input = valid_input(today);
        input.name = "".to_string();
        let err = validate_task_input(&input, today).unwrap_err();
        assert!(err.contains("nameは必須です"), "got={}", err);
    }

    #[test]
    fn validate_task_input_rejects_name_over_20_chars() {
        let today = NaiveDate::from_ymd_opt(2026, 9, 9).unwrap();
        let mut input = valid_input(today);
        input.name = "a".repeat(21);
        let err = validate_task_input(&input, today).unwrap_err();
        assert!(err.contains("20文字以内"), "got={}", err);
    }

    #[test]
    fn validate_task_input_accepts_name_exactly_20_chars() {
        let today = NaiveDate::from_ymd_opt(2026, 9, 9).unwrap();
        let mut input = valid_input(today);
        input.name = "a".repeat(20);
        assert!(validate_task_input(&input, today).is_ok());
    }

    // 【2回目のテスト監査で追加】backend(Go)は`len([]rune(name))`(コードポイント数)で20文字を
    // 判定している。比較実装側で「バイト数」や(Scalaのような)「UTF-16コード単位数」で数えてしまうと、
    // 基本多言語面外の文字(絵文字等)を含む名前で契約違反が起きる(実際にbackend-scala-http4sで
    // この種の不一致が見つかった、TaskService.scalaのコメント参照)。Rust側は`.chars().count()`
    // (コードポイント数)を使っており正しいはずだが、この境界値がテストされていなかったため追加する。
    #[test]
    fn validate_task_input_accepts_astral_emoji_name_of_20_codepoints() {
        let today = NaiveDate::from_ymd_opt(2026, 9, 9).unwrap();
        let mut input = valid_input(today);
        input.name = "😀".repeat(20); // コードポイント数20、バイト数は80(4バイト/文字)
        assert!(
            validate_task_input(&input, today).is_ok(),
            "コードポイント数20の絵文字名が拒否された(バイト数で誤判定していないか確認)"
        );
    }

    #[test]
    fn validate_task_input_rejects_astral_emoji_name_of_21_codepoints() {
        let today = NaiveDate::from_ymd_opt(2026, 9, 9).unwrap();
        let mut input = valid_input(today);
        input.name = "😀".repeat(21);
        let err = validate_task_input(&input, today).unwrap_err();
        assert!(err.contains("20文字以内"), "got={}", err);
    }

    #[test]
    fn validate_task_input_rejects_past_finished_on() {
        let today = NaiveDate::from_ymd_opt(2026, 9, 9).unwrap();
        let mut input = valid_input(today);
        input.finished_on = today.pred_opt().unwrap();
        let err = validate_task_input(&input, today).unwrap_err();
        assert!(err.contains("過去日"), "got={}", err);
    }

    #[test]
    fn validate_task_input_accepts_finished_on_equal_to_today() {
        let today = NaiveDate::from_ymd_opt(2026, 9, 9).unwrap();
        let input = valid_input(today);
        assert!(validate_task_input(&input, today).is_ok());
    }

    #[test]
    fn validate_task_input_rejects_unknown_status() {
        let today = NaiveDate::from_ymd_opt(2026, 9, 9).unwrap();
        let mut input = valid_input(today);
        input.status = "not_a_status".to_string();
        let err = validate_task_input(&input, today).unwrap_err();
        assert!(err.contains("不明なstatus"), "got={}", err);
    }

    #[test]
    fn task_status_round_trips_through_string_and_db_value() {
        for (status, s, db) in [
            (TaskStatus::Waiting, "waiting", 1u8),
            (TaskStatus::WorkInProgress, "work_in_progress", 2u8),
            (TaskStatus::Completed, "completed", 3u8),
        ] {
            assert_eq!(status.as_str(), s);
            assert_eq!(TaskStatus::from_str(s), Some(status));
            assert_eq!(TaskStatus::from_db(db), Some(status));
        }
        assert_eq!(TaskStatus::from_str("bogus"), None);
        assert_eq!(TaskStatus::from_db(0), None);
        assert_eq!(TaskStatus::from_db(99), None);
    }
}
