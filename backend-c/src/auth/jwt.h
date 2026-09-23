#ifndef AUTH_JWT_H
#define AUTH_JWT_H

#include <stdbool.h>
#include <stddef.h>

struct cJSON;

/* backend-rust(src/auth/jwt.rs)・backend-cpp(src/auth/jwt.hpp)と同一のissuer文字列 */
#define AUTH_LOCAL_HMAC_ISSUER "bff-gin-local-hmac"
#define AUTH_LOCAL_RSA_ISSUER "bff-gin-local-rsa"

bool auth_is_local_issuer(const char *iss);

/* user_id解決(auth/user_resolver.h)・外部公開APIのクライアント検証(auth/external_auth.h)に
 * 必要な最小限のクレームのみ保持する(backend-cpp::auth::ClaimsのrealmRolesは
 * どちらの用途にも使わないため省略している) */
typedef struct {
    char *sub;
    char *iss;
    char *azp;
} Claims;

void claims_destroy(Claims *claims);

/*
 * "header.payload.signature"を3分割する。各フィールドはtoken内を指すポインタ+長さのみ
 * (コピーしない)。呼び出し側はtokenが生存している間だけJwtPartsを使うこと
 * 戻り値: 成功時0、区切りが2つ無ければ-1
 */
typedef struct {
    const char *header_b64;
    size_t header_b64_len;
    const char *payload_b64;
    size_t payload_b64_len;
    const char *signature_b64;
    size_t signature_b64_len;
    const char *signing_input; /* token先頭から"header.payload"まで(署名対象) */
    size_t signing_input_len;
} JwtParts;

int jwt_split(const char *token, JwtParts *out_parts);

/* payload部をbase64url decode+JSON parseする。戻り値は呼び出し側がcJSON_Delete()すること
 * (署名検証前にissだけ覗く用途(Dispatcher)、およびexp/aud/iss検証(各Verifier)の両方で使う) */
struct cJSON *jwt_decode_payload_json(const char *payload_b64, size_t payload_b64_len);

/* header部の"alg"文字列を返す(呼び出し側がfree()すること)。無ければNULL */
char *jwt_decode_header_alg(const char *header_b64, size_t header_b64_len);
/* header部の"kid"文字列を返す(呼び出し側がfree()すること)。無ければNULL */
char *jwt_decode_header_kid(const char *header_b64, size_t header_b64_len);

/* payload JSONからClaims(sub/iss)を作る。呼び出し側がclaims_destroy()すること */
Claims *jwt_claims_from_json(const struct cJSON *payload_json);

/*
 * iss完全一致・aud一致(RFC7519によりaudは文字列または文字列配列のどちらもありうる、
 * いずれかがexpected_audienceと一致すればよい)・exp未失効(time(NULL)基準)を確認する
 */
bool jwt_claims_valid(const struct cJSON *payload_json, const char *expected_issuer,
                      const char *expected_audience);

#endif /* AUTH_JWT_H */
