//! backend(Go)の internal/service/task_external.go の
//! encodeExternalCursor/decodeExternalCursor と同じ規約:
//! `(created_at, id)` を `"<RFC3339Nano>|<id>"` にしてbase64(パディング付きURL-safe、
//! Goの`base64.URLEncoding`と同じ)でエンコードした不透明文字列

use base64::Engine;
use chrono::{DateTime, NaiveDateTime, Utc};

pub fn encode(created_at: NaiveDateTime, id: u64) -> String {
    let raw = format!("{}|{}", created_at.and_utc().to_rfc3339(), id);
    base64::engine::general_purpose::URL_SAFE.encode(raw.as_bytes())
}

pub fn decode(cursor: &str) -> Result<(NaiveDateTime, u64), String> {
    let raw = base64::engine::general_purpose::URL_SAFE
        .decode(cursor)
        .map_err(|e| format!("base64デコード失敗: {}", e))?;
    let raw = String::from_utf8(raw).map_err(|e| format!("UTF-8デコード失敗: {}", e))?;
    let mut parts = raw.splitn(2, '|');
    let created_at_str = parts.next().ok_or("cursorの区切りが不正です")?;
    let id_str = parts.next().ok_or("cursorの区切りが不正です")?;

    let created_at = DateTime::parse_from_rfc3339(created_at_str)
        .map_err(|e| format!("created_atの形式が不正です: {}", e))?
        .with_timezone(&Utc)
        .naive_utc();
    let id: u64 = id_str
        .parse()
        .map_err(|e| format!("idの形式が不正です: {}", e))?;
    Ok((created_at, id))
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::NaiveDate;

    #[test]
    fn encode_decode_round_trip() {
        let created_at = NaiveDate::from_ymd_opt(2026, 9, 9)
            .unwrap()
            .and_hms_opt(12, 34, 56)
            .unwrap();
        let cursor = encode(created_at, 42);
        let (decoded_created_at, decoded_id) = decode(&cursor).expect("decode should succeed");
        assert_eq!(decoded_created_at, created_at);
        assert_eq!(decoded_id, 42);
    }

    #[test]
    fn decode_rejects_garbage() {
        assert!(decode("not-a-valid-cursor!!!").is_err());
    }

    #[test]
    fn decode_rejects_wrong_separator_count() {
        let bogus = base64::engine::general_purpose::URL_SAFE.encode(b"just-one-part");
        assert!(decode(&bogus).is_err());
    }
}
