#ifndef AUTH_JWKS_VERIFIER_H
#define AUTH_JWKS_VERIFIER_H

#include "auth/jwt.h"

/*
 * JWKSベースのRS256検証。Keycloak発行・ローカルRSA発行(iss=bff-gin-local-rsa)の両方に
 * 使い回す(jwks_url/issuer/audienceが違うだけの2インスタンスを作ればよい)。
 * kid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけJWKSを再取得する
 * (backend(Go)のjwks.go・backend-rust/backend-cppのJwksVerifierと同じ「kid不一致時のみ
 * 再取得」戦略)
 */
typedef struct JwksVerifier JwksVerifier;

JwksVerifier *jwks_verifier_create(const char *jwks_url, const char *issuer, const char *audience);
void jwks_verifier_destroy(JwksVerifier *v);

/* auth/dispatcher.hのVerifierFnと一致するシグネチャ(selfはJwksVerifier*にキャストする)
 * 戻り値: 成功時0+*out_claims(呼び出し側がclaims_destroy)、失敗時-1 */
int jwks_verifier_verify(void *self, const char *token, Claims **out_claims);

#endif /* AUTH_JWKS_VERIFIER_H */
