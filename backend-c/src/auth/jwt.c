#include "auth/jwt.h"

#include <cjson/cJSON.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "auth/base64url.h"

bool auth_is_local_issuer(const char *iss) {
    return strcmp(iss, AUTH_LOCAL_HMAC_ISSUER) == 0 || strcmp(iss, AUTH_LOCAL_RSA_ISSUER) == 0;
}

void claims_destroy(Claims *claims) {
    if (claims == NULL) return;
    free(claims->sub);
    free(claims->iss);
    free(claims->azp);
    free(claims);
}

int jwt_split(const char *token, JwtParts *out_parts) {
    const char *p1 = strchr(token, '.');
    if (p1 == NULL) return -1;
    const char *p2 = strchr(p1 + 1, '.');
    if (p2 == NULL) return -1;

    out_parts->header_b64 = token;
    out_parts->header_b64_len = (size_t)(p1 - token);
    out_parts->payload_b64 = p1 + 1;
    out_parts->payload_b64_len = (size_t)(p2 - p1 - 1);
    out_parts->signature_b64 = p2 + 1;
    out_parts->signature_b64_len = strlen(p2 + 1);
    out_parts->signing_input = token;
    out_parts->signing_input_len = (size_t)(p2 - token);
    return 0;
}

static cJSON *decode_json_part(const char *b64, size_t b64_len) {
    size_t raw_len = 0;
    uint8_t *raw = base64url_decode(b64, b64_len, &raw_len);
    if (raw == NULL) return NULL;
    cJSON *json = cJSON_ParseWithLength((const char *)raw, raw_len);
    free(raw);
    return json;
}

cJSON *jwt_decode_payload_json(const char *payload_b64, size_t payload_b64_len) {
    return decode_json_part(payload_b64, payload_b64_len);
}

static char *decode_header_string_field(const char *header_b64, size_t header_b64_len,
                                         const char *field) {
    cJSON *header = decode_json_part(header_b64, header_b64_len);
    if (header == NULL) return NULL;
    cJSON *value = cJSON_GetObjectItemCaseSensitive(header, field);
    char *out = (cJSON_IsString(value) && value->valuestring != NULL) ? strdup(value->valuestring)
                                                                       : NULL;
    cJSON_Delete(header);
    return out;
}

char *jwt_decode_header_alg(const char *header_b64, size_t header_b64_len) {
    return decode_header_string_field(header_b64, header_b64_len, "alg");
}

char *jwt_decode_header_kid(const char *header_b64, size_t header_b64_len) {
    return decode_header_string_field(header_b64, header_b64_len, "kid");
}

Claims *jwt_claims_from_json(const cJSON *payload_json) {
    Claims *c = (Claims *)calloc(1, sizeof(Claims));
    if (c == NULL) return NULL;

    cJSON *sub = cJSON_GetObjectItemCaseSensitive(payload_json, "sub");
    cJSON *iss = cJSON_GetObjectItemCaseSensitive(payload_json, "iss");
    cJSON *azp = cJSON_GetObjectItemCaseSensitive(payload_json, "azp");
    c->sub = strdup(cJSON_IsString(sub) && sub->valuestring != NULL ? sub->valuestring : "");
    c->iss = strdup(cJSON_IsString(iss) && iss->valuestring != NULL ? iss->valuestring : "");
    /* azpクレームは全issuerに必須ではない(ローカルHMAC/RSA発行のJWTは通常持たない)。
     * 無ければ空文字列にしておくことで、外部公開APIのazp一致チェックが自然に失敗する
     * (未設定のazpが特定のclient_idと偶然一致することはない) */
    c->azp = strdup(cJSON_IsString(azp) && azp->valuestring != NULL ? azp->valuestring : "");
    if (c->sub == NULL || c->iss == NULL || c->azp == NULL) {
        claims_destroy(c);
        return NULL;
    }
    return c;
}

bool jwt_claims_valid(const cJSON *payload_json, const char *expected_issuer,
                      const char *expected_audience) {
    cJSON *iss = cJSON_GetObjectItemCaseSensitive(payload_json, "iss");
    if (!cJSON_IsString(iss) || iss->valuestring == NULL ||
        strcmp(iss->valuestring, expected_issuer) != 0) {
        return false;
    }

    /* audはRFC7519上、文字列1個または文字列配列のどちらもありうる。どちらの形でも
     * expected_audienceのいずれかと一致すればよい(backend-cppのAudienceMatchesと同じ) */
    cJSON *aud = cJSON_GetObjectItemCaseSensitive(payload_json, "aud");
    bool aud_ok = false;
    if (cJSON_IsString(aud) && aud->valuestring != NULL) {
        aud_ok = strcmp(aud->valuestring, expected_audience) == 0;
    } else if (cJSON_IsArray(aud)) {
        cJSON *item = NULL;
        cJSON_ArrayForEach(item, aud) {
            if (cJSON_IsString(item) && item->valuestring != NULL &&
                strcmp(item->valuestring, expected_audience) == 0) {
                aud_ok = true;
                break;
            }
        }
    }
    if (!aud_ok) return false;

    cJSON *exp = cJSON_GetObjectItemCaseSensitive(payload_json, "exp");
    if (!cJSON_IsNumber(exp)) return false;
    double now = (double)time(NULL);
    if (now >= exp->valuedouble) return false;

    return true;
}
