package com.bffgin.backend.auth;

import io.jsonwebtoken.Claims;

/**
 * audはRFC7519上、文字列1個または文字列配列のどちらもありうる。
 * jjwt 0.12+のClaims#getAudienceはどちらの形でもSet&lt;String&gt;として正規化して返すため、
 * ここでは単純にcontainsで確認すればよい(backend-cpp/backend-cのAudienceMatches相当)
 */
final class AudienceMatcher {
    private AudienceMatcher() {
    }

    static boolean matches(Claims claims, String expected) {
        var audience = claims.getAudience();
        return audience != null && audience.contains(expected);
    }
}
