package com.bffgin.backend.auth

/**
 * user_id解決(UserResolver)にはsub/issのみ必要だが、外部公開API
 * (RequireExternalClientAuth相当、ExternalAuth.kt参照)はazp(authorized party)クレームで
 * Client Credentials Grantのクライアントを識別する必要があるため保持する。
 * azpクレームが無いトークン(ローカルHMAC/RSA発行分は通常持たない)は空文字列になる
 */
data class Claims(val sub: String, val iss: String, val azp: String = "")
