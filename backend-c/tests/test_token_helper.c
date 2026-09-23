#include "test_token_helper.h"

#include <cjson/cJSON.h>
#include <openssl/evp.h>
#include <openssl/hmac.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "auth/base64url.h"

static char *json_to_b64(cJSON *json) {
    char *raw = cJSON_PrintUnformatted(json);
    char *encoded = base64url_encode((const uint8_t *)raw, strlen(raw));
    free(raw);
    return encoded;
}

static char *concat_with_dot(const char *a, const char *b) {
    size_t len = strlen(a) + 1 + strlen(b);
    char *out = (char *)malloc(len + 1);
    snprintf(out, len + 1, "%s.%s", a, b);
    return out;
}

static cJSON *build_payload_ext(const char *iss, const char *aud, const char *sub,
                                 const char *azp, long long exp_offset_secs) {
    cJSON *payload = cJSON_CreateObject();
    cJSON_AddStringToObject(payload, "sub", sub);
    cJSON_AddStringToObject(payload, "iss", iss);
    cJSON_AddStringToObject(payload, "aud", aud);
    if (azp != NULL && azp[0] != '\0') {
        cJSON_AddStringToObject(payload, "azp", azp);
    }
    cJSON_AddNumberToObject(payload, "exp", (double)(time(NULL) + exp_offset_secs));
    return payload;
}

static cJSON *build_payload(const char *iss, const char *aud, const char *sub,
                             long long exp_offset_secs) {
    return build_payload_ext(iss, aud, sub, NULL, exp_offset_secs);
}

char *make_hmac_token(const char *secret, const char *iss, const char *aud, const char *sub,
                       long long exp_offset_secs) {
    cJSON *header = cJSON_CreateObject();
    cJSON_AddStringToObject(header, "alg", "HS256");
    cJSON_AddStringToObject(header, "typ", "JWT");
    char *header_b64 = json_to_b64(header);
    cJSON_Delete(header);

    cJSON *payload = build_payload(iss, aud, sub, exp_offset_secs);
    char *payload_b64 = json_to_b64(payload);
    cJSON_Delete(payload);

    char *signing_input = concat_with_dot(header_b64, payload_b64);

    unsigned char mac[EVP_MAX_MD_SIZE];
    unsigned int mac_len = 0;
    HMAC(EVP_sha256(), secret, (int)strlen(secret), (const unsigned char *)signing_input,
         strlen(signing_input), mac, &mac_len);
    char *sig_b64 = base64url_encode(mac, mac_len);

    char *token = concat_with_dot(signing_input, sig_b64);

    free(header_b64);
    free(payload_b64);
    free(signing_input);
    free(sig_b64);
    return token;
}

char *make_rsa_token(EVP_PKEY *private_key, const char *kid, const char *iss, const char *aud,
                      const char *sub, long long exp_offset_secs) {
    return make_rsa_token_with_azp(private_key, kid, iss, aud, sub, NULL, exp_offset_secs);
}

char *make_rsa_token_with_azp(EVP_PKEY *private_key, const char *kid, const char *iss,
                               const char *aud, const char *sub, const char *azp,
                               long long exp_offset_secs) {
    cJSON *header = cJSON_CreateObject();
    cJSON_AddStringToObject(header, "alg", "RS256");
    cJSON_AddStringToObject(header, "typ", "JWT");
    cJSON_AddStringToObject(header, "kid", kid);
    char *header_b64 = json_to_b64(header);
    cJSON_Delete(header);

    cJSON *payload = build_payload_ext(iss, aud, sub, azp, exp_offset_secs);
    char *payload_b64 = json_to_b64(payload);
    cJSON_Delete(payload);

    char *signing_input = concat_with_dot(header_b64, payload_b64);

    unsigned char sig[512];
    size_t sig_len = sizeof(sig);
    char *sig_b64 = NULL;
    EVP_MD_CTX *ctx = EVP_MD_CTX_new();
    if (ctx != NULL) {
        if (EVP_DigestSignInit(ctx, NULL, EVP_sha256(), NULL, private_key) == 1 &&
            EVP_DigestSign(ctx, sig, &sig_len, (const unsigned char *)signing_input,
                            strlen(signing_input)) == 1) {
            sig_b64 = base64url_encode(sig, sig_len);
        }
        EVP_MD_CTX_free(ctx);
    }

    char *token = (sig_b64 != NULL) ? concat_with_dot(signing_input, sig_b64) : NULL;

    free(header_b64);
    free(payload_b64);
    free(signing_input);
    free(sig_b64);
    return token;
}
