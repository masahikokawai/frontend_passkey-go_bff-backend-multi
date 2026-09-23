package com.bffgin.backend.flags;

import javax.sql.DataSource;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

/**
 * feature_flagsテーブルを10秒間隔で直接ポーリングする(bffのようなHTTPポーリングではない、
 * backend-c/backend-cpp/backend-rustと同じ設計)。外部公開API(backend.external-tasks-pagination-v2)
 * の判定に使う。
 *
 * 読み取り側(variation)がロック無しで安全に読めるよう、キャッシュ全体を不変Mapとして
 * volatileフィールドへ丸ごと差し替える(ポーリングスレッドだけが書き込み、他スレッドは
 * 常に一貫したスナップショットを読む。Java実装のVirtual Threadsと同じ「共有可変状態を
 * 極力減らす」設計方針)
 */
public final class FeatureFlagPoller implements AutoCloseable {

    public record Entry(boolean enabled, String defaultVariation) {
    }

    private final DataSource dataSource;
    private final ScheduledExecutorService scheduler = Executors.newSingleThreadScheduledExecutor(
            r -> {
                Thread t = new Thread(r, "feature-flag-poller");
                t.setDaemon(true);
                return t;
            });
    private volatile Map<String, Entry> entries = Map.of();

    public FeatureFlagPoller(DataSource dataSource) {
        this.dataSource = dataSource;
    }

    public void start() {
        pollOnce();
        scheduler.scheduleAtFixedRate(this::pollOnce, 10, 10, TimeUnit.SECONDS);
    }

    public void pollOnce() {
        Map<String, Entry> next = new HashMap<>();
        try (Connection conn = dataSource.getConnection();
                PreparedStatement ps = conn.prepareStatement(
                        "SELECT flag_key, enabled, default_variation FROM feature_flags");
                ResultSet rs = ps.executeQuery()) {
            while (rs.next()) {
                next.put(rs.getString("flag_key"), new Entry(rs.getBoolean("enabled"), rs.getString("default_variation")));
            }
            entries = Map.copyOf(next);
        } catch (SQLException e) {
            // 【バッド/グッドプラクティス】1回のポーリング失敗で例外を伝播させ
            // スケジューラ自体を止めてしまうと、以後永久にフラグが更新されなくなる。
            // ここでは失敗を握りつぶし、既存のキャッシュ(直前の正常な取得結果)を
            // 保持したまま次回のポーリングに委ねる
        }
    }

    /** enabled=falseの場合、または未知のflagKeyの場合はdefaultValueを返す */
    public String variation(String flagKey, String defaultValue) {
        Entry entry = entries.get(flagKey);
        if (entry == null || !entry.enabled()) {
            return defaultValue;
        }
        return entry.defaultVariation();
    }

    @Override
    public void close() {
        scheduler.shutdownNow();
    }
}
