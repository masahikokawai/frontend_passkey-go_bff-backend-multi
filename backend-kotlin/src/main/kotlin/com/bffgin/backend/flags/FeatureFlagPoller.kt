package com.bffgin.backend.flags

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.sql.DataSource

/**
 * feature_flagsテーブルを10秒間隔で直接ポーリングする(bffのようなHTTPポーリングではない、
 * backend-java/backend-rust/backend-cpp/backend-cと同じ設計)。外部公開API
 * (backend.external-tasks-pagination-v2)の判定にのみ使う。
 *
 * 【このKotlin実装の一貫した設計原則をポーラーにも適用する】TaskRepository同様、
 * ここでのJDBC呼び出しも`withContext(Dispatchers.IO)`で明示的に囲む。ポーリングという
 * バックグラウンド処理だからといって、この設計原則(ブロッキングI/Oは呼び出し側が
 * 自己申告してディスパッチャを明示的に切り替える)の例外にはしない。
 *
 * キャッシュは`@Volatile`な不変Mapを毎回まるごと置き換える方式。JVMメモリモデル上、
 * `@Volatile`フィールドへの書き込みはそれ以前の全ての書き込みと同期し、他スレッドから見て
 * 安全に公開される(safe publication)ため、読み取り側にロックは不要
 */
class FeatureFlagPoller(private val dataSource: DataSource) {

    private data class Entry(val enabled: Boolean, val defaultVariation: String)

    @Volatile
    private var cache: Map<String, Entry> = emptyMap()

    private var job: Job? = null

    fun start(scope: CoroutineScope) {
        job = scope.launch {
            while (isActive) {
                pollOnce()
                delay(10_000)
            }
        }
    }

    fun stop() {
        job?.cancel()
    }

    /** enabled=trueかつ見つかった場合はdefault_variation、それ以外は呼び出し側のdefaultValue */
    fun variation(flagKey: String, defaultValue: String): String {
        val entry = cache[flagKey] ?: return defaultValue
        return if (entry.enabled) entry.defaultVariation else defaultValue
    }

    /** 通常は10秒間隔の内部ループから呼ばれるが、テストでは即時実行のために直接呼び出す(公開) */
    suspend fun pollOnce() {
        val newCache = withContext(Dispatchers.IO) {
            val result = mutableMapOf<String, Entry>()
            dataSource.connection.use { conn ->
                conn.prepareStatement("SELECT flag_key, enabled, default_variation FROM feature_flags").use { ps ->
                    ps.executeQuery().use { rs ->
                        while (rs.next()) {
                            result[rs.getString("flag_key")] = Entry(rs.getBoolean("enabled"), rs.getString("default_variation"))
                        }
                    }
                }
            }
            result
        }
        cache = newCache
    }
}
