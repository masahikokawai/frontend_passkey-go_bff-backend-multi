#ifndef AUTH_DISPATCHER_H
#define AUTH_DISPATCHER_H

#include "auth/jwt.h"

typedef struct Dispatcher Dispatcher;

/*
 * HmacVerifier/JwksVerifierを型消去して保持するための共通シグネチャ。selfには
 * dispatcher_register()に渡したverifier_selfポインタがそのまま渡される
 * (hmac_verifier_verify/jwks_verifier_verifyはどちらもこの型に一致する)
 */
typedef int (*VerifierFn)(void *self, const char *token, Claims **out_claims);

Dispatcher *dispatcher_create(void);
void dispatcher_destroy(Dispatcher *d);

/* 戻り値: 成功時0、登録上限超過またはmalloc失敗時-1 */
int dispatcher_register(Dispatcher *d, const char *issuer, void *verifier_self,
                         VerifierFn verify_fn);

/*
 * Authorizationヘッダの値そのもの("Bearer xxx")を渡す。署名検証前にissだけ覗いて
 * 登録済みissuerへ振り分け、実際の検証(署名・exp・aud)はそのverifierへ委譲する
 * (backend(Go)のauthjwt.Dispatcher・backend-rust/backend-cppのDispatcherと同じ2段構造)
 * 戻り値: 成功時0+*out_claims(呼び出し側がclaims_destroy)、失敗時-1
 */
int dispatcher_verify(Dispatcher *d, const char *authorization_header, Claims **out_claims);

#endif /* AUTH_DISPATCHER_H */
