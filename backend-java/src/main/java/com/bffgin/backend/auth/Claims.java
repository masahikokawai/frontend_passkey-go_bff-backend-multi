package com.bffgin.backend.auth;

/**
 * user_id解決(UserResolver)に必要な最小限のクレームに加え、外部公開API向けの`azp`
 * (authorized party、Client Credentials Grantのクライアントid)も保持する。
 * 内部REST/gRPCのuser_id解決にはazpは使わないため、Phase 1では省略していた
 */
public record Claims(String sub, String iss, String azp) {
}
