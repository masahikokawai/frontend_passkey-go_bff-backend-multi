package com.bffgin.backend.auth

/**
 * suspend funにしているのは、JwksVerifier実装がkid不一致時にJWKSをネットワーク越しに
 * 再取得する(Ktor Clientのsuspend呼び出し)必要があるため。HmacVerifierは実際には
 * 何も一時停止しない(jjwtの同期検証のみ)が、インターフェースは統一している
 */
fun interface TokenVerifier {
    suspend fun verify(token: String): Claims
}
