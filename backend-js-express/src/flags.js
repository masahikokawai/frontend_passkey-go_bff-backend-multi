'use strict';

// feature_flags テーブルを直接評価する(backend(Go)のinternal/featureflag/mysql_retriever.goと
// 同じ考え方: backendは元々DB接続を持つため、bff/gatewayのようなHTTPポーリングではなく
// MySQLを正本として直接読む)。今回必要なのは`backend.external-tasks-pagination-v2`の
// bool値だけであり、この1フラグはGo/Rust/Scala×2/Rails/JS/TSの各言語で共有する
// (CONTRACT.mdセクション20)ため、GO Feature Flag SDK相当のフルスタックは持ち込まず、
// 素朴なポーリングキャッシュにとどめる(backend-rustのflags.rsと同じ設計)

// backend(Go)のBuildFlagConfigJSON/GO Feature Flagの評価規則を素朴に再現する:
//   - enabled = false なら false
//   - enabled = true なら variations[default_variation] を bool として使う
//     (variations列が無い/パース失敗時は{"on":true,"off":false}のフォールバックを使う)
async function fetchBool(pool, flagKey) {
  const [rows] = await pool.execute(
    'SELECT enabled, default_variation, variations FROM feature_flags WHERE flag_key = ?',
    [flagKey],
  );
  if (rows.length === 0) return null;
  const row = rows[0];
  const enabled = !!row.enabled;
  if (!enabled) return false;

  const defaultVariation = row.default_variation || '';
  let variations;
  try {
    variations = row.variations ? JSON.parse(row.variations) : { on: true, off: false };
  } catch (e) {
    variations = { on: true, off: false };
  }
  const v = variations[defaultVariation];
  return typeof v === 'boolean' ? v : false;
}

// 起動時に1回同期的に読み込み、以降はバックグラウンドでpollIntervalMsごとに再読込する
// (Goの`FeatureFlagPollInterval`既定10秒と合わせる)
class FlagCache {
  constructor(flagKey) {
    this.flagKey = flagKey;
    this.value = false;
    this._timer = null;
  }

  static async spawn(pool, flagKey, pollIntervalMs) {
    const cache = new FlagCache(flagKey);
    const initial = await fetchBool(pool, flagKey);
    if (initial !== null) cache.value = initial;

    cache._timer = setInterval(() => {
      fetchBool(pool, flagKey)
        .then((v) => {
          if (v !== null) cache.value = v;
        })
        .catch(() => {
          // ポーリング失敗時は直前の値を保持する(backend-rustと同じ、フェイルセーフ)
        });
    }, pollIntervalMs);
    // このタイマーだけでプロセスの終了がブロックされないようにする
    if (cache._timer.unref) cache._timer.unref();

    return cache;
  }

  get() {
    return this.value;
  }
}

module.exports = { fetchBool, FlagCache };
