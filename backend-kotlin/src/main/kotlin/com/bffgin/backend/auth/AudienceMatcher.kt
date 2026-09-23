package com.bffgin.backend.auth

import io.jsonwebtoken.Claims as JjwtClaims

/**
 * audはRFC7519上、文字列1個または文字列配列のどちらもありうる。
 * jjwt 0.12+のClaims#getAudienceはどちらの形でもSet<String>として正規化して返すため、
 * ここでは単純にcontainsで確認すればよい(backend-java/backend-cpp/backend-cのAudienceMatches相当)
 */
internal object AudienceMatcher {
    fun matches(claims: JjwtClaims, expected: String): Boolean {
        val audience = claims.audience
        return audience != null && audience.contains(expected)
    }
}
