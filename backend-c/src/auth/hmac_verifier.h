#ifndef AUTH_HMAC_VERIFIER_H
#define AUTH_HMAC_VERIFIER_H

#include "auth/jwt.h"

/* ローカルHMAC発行(iss=bff-gin-local-hmac)トークンの検証。HS256、共有シークレット */
typedef struct HmacVerifier HmacVerifier;

HmacVerifier *hmac_verifier_create(const char *secret, const char *issuer, const char *audience);
void hmac_verifier_destroy(HmacVerifier *v);

/* auth/dispatcher.hのVerifierFnと一致するシグネチャ(selfはHmacVerifier*にキャストする)
 * 戻り値: 成功時0+*out_claims(呼び出し側がclaims_destroy)、失敗時-1 */
int hmac_verifier_verify(void *self, const char *token, Claims **out_claims);

#endif /* AUTH_HMAC_VERIFIER_H */
