package com.bffgin.backend.external;

import com.bffgin.backend.domain.TaskError;

import java.util.Map;
import java.util.Optional;

/**
 * 外部公開API(/external/v1/tasks)のクエリパラメータ解析だけを担う、副作用の無い純粋な
 * ヘルパー(実サーバーを起動せずに単体テストできるよう、RestErrorMapperと同じ理由で
 * ハンドラ本体から切り出している)
 */
public final class ExternalQuery {

    private ExternalQuery() {
    }

    public record OffsetParams(int page, int pageSize) {
    }

    public record CursorParams(long afterId, int limit) {
    }

    public static long parseUserId(Map<String, String> params) throws TaskError {
        String raw = params.get("user_id");
        if (raw == null || raw.isEmpty()) {
            throw TaskError.userIdRequired();
        }
        try {
            return Long.parseLong(raw);
        } catch (NumberFormatException e) {
            throw TaskError.invalidUserId();
        }
    }

    public static OffsetParams parseOffsetParams(Map<String, String> params) {
        int page = parsePositiveInt(params.get("page"), 1);
        int pageSize = parsePositiveInt(params.get("page_size"), 10);
        return new OffsetParams(page, pageSize);
    }

    public static CursorParams parseCursorParams(Map<String, String> params) {
        String cursorRaw = params.get("cursor");
        long afterId = 0;
        if (cursorRaw != null && !cursorRaw.isEmpty()) {
            try {
                afterId = Long.parseLong(cursorRaw);
            } catch (NumberFormatException ignored) {
                afterId = 0;
            }
        }
        int limit = parsePositiveInt(params.get("limit"), 10);
        return new CursorParams(Math.max(afterId, 0), limit);
    }

    private static int parsePositiveInt(String raw, int defaultValue) {
        if (raw == null || raw.isEmpty()) {
            return defaultValue;
        }
        try {
            int v = Integer.parseInt(raw);
            return Math.max(v, 1);
        } catch (NumberFormatException e) {
            return defaultValue;
        }
    }

    public static Optional<String> nextCursor(long lastId) {
        return lastId > 0 ? Optional.of(Long.toString(lastId)) : Optional.empty();
    }
}
