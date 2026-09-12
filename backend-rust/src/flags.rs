//! feature_flags テーブルを直接評価する(backend(Go)のinternal/featureflag/mysql_retriever.goと
//! 同じ考え方: backendは元々DB接続を持つため、bff/gatewayのようなHTTPポーリングではなく
//! MySQLを正本として直接読む)。今回必要なのは`backend.external-tasks-pagination-v2`の
//! bool値だけであり、この1フラグはGo/Rust/Scala(http4s)/Scala(Pekko)/Railsの5言語で
//! 共有する(CONTRACT.mdセクション20、ユーザーの明示的な指示)ため、GO Feature Flag SDK相当の
//! フルスタックは持ち込まず、素朴なポーリングキャッシュにとどめる。

use serde_json::Value;
use sqlx::mysql::MySqlPool;
use sqlx::Row;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

/// backend(Go)のBuildFlagConfigJSON/GO Feature Flagの評価規則を素朴に再現する:
///   - enabled = false なら false(SDKのdisable相当。呼び出し側の既定値もfalseなので同じ)
///   - enabled = true なら variations[default_variation] を bool として使う
///     (variations列が無い/パース失敗時は{"on":true,"off":false}のフォールバックを使う。
///     セクション19のバックフィルと同じ規約)
pub async fn fetch_bool(pool: &MySqlPool, flag_key: &str) -> Option<bool> {
    let row = sqlx::query(
        "SELECT enabled, default_variation, variations FROM feature_flags WHERE flag_key = ?",
    )
    .bind(flag_key)
    .fetch_optional(pool)
    .await
    .ok()??;

    let enabled: bool = row.try_get::<i8, _>("enabled").map(|v| v != 0).unwrap_or(false);
    if !enabled {
        return Some(false);
    }

    let default_variation: String = row.try_get("default_variation").unwrap_or_default();
    let variations_raw: Option<String> = row.try_get("variations").ok();

    let variations: Value = variations_raw
        .filter(|s| !s.is_empty())
        .and_then(|s| serde_json::from_str(&s).ok())
        .unwrap_or_else(|| serde_json::json!({"on": true, "off": false}));

    Some(
        variations
            .get(&default_variation)
            .and_then(|v| v.as_bool())
            .unwrap_or(false),
    )
}

/// 起動時に1回同期的に読み込み、以降はバックグラウンドタスクで
/// `poll_interval`ごとに再読込する(Goの`FeatureFlagPollInterval`既定10秒と合わせる)
pub struct FlagCache {
    flag_key: String,
    value: Arc<AtomicBool>,
}

impl FlagCache {
    pub async fn spawn(pool: MySqlPool, flag_key: &str, poll_interval: Duration) -> Arc<Self> {
        let value = Arc::new(AtomicBool::new(false));
        if let Some(v) = fetch_bool(&pool, flag_key).await {
            value.store(v, Ordering::Relaxed);
        }
        let cache = Arc::new(Self {
            flag_key: flag_key.to_string(),
            value: value.clone(),
        });
        let cache_bg = cache.clone();
        tokio::spawn(async move {
            let mut interval = tokio::time::interval(poll_interval);
            interval.tick().await; // 1回目は起動直後の同期読み込みと重複するので捨てる
            loop {
                interval.tick().await;
                if let Some(v) = fetch_bool(&pool, &cache_bg.flag_key).await {
                    cache_bg.value.store(v, Ordering::Relaxed);
                }
            }
        });
        cache
    }

    pub fn get(&self) -> bool {
        self.value.load(Ordering::Relaxed)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // fetch_bool自体はDB接続が要るため統合テスト側(tests/)で確認する。
    // ここではvariations解決の純粋なロジック部分だけを切り出して確認する
    #[test]
    fn variations_lookup_resolves_default_variation_to_bool() {
        let variations = serde_json::json!({"on": true, "off": false});
        assert_eq!(variations.get("on").and_then(|v| v.as_bool()), Some(true));
        assert_eq!(variations.get("off").and_then(|v| v.as_bool()), Some(false));
        assert_eq!(variations.get("missing").and_then(|v| v.as_bool()), None);
    }

    #[test]
    fn variations_fallback_used_when_column_empty() {
        let variations_raw: Option<String> = None;
        let variations: Value = variations_raw
            .filter(|s: &String| !s.is_empty())
            .and_then(|s| serde_json::from_str(&s).ok())
            .unwrap_or_else(|| serde_json::json!({"on": true, "off": false}));
        assert_eq!(variations, serde_json::json!({"on": true, "off": false}));
    }
}
