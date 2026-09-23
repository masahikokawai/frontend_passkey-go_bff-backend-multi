#include "auth/dispatcher.h"

#include <cjson/cJSON.h>
#include <stdlib.h>
#include <string.h>

/* issuerは3つ(ローカルHMAC/ローカルRSA/Keycloak)固定でこれ以上増える予定が無いため、
 * ハッシュマップは使わず固定長配列の線形探索にしている(backend-rust/backend-cppの
 * HashMap/std::mapに対する意図的な簡略化。件数が数個であれば計算量上のデメリットは無い) */
#define DISPATCHER_MAX_ENTRIES 8

typedef struct {
    char *issuer;
    void *verifier_self;
    VerifierFn verify_fn;
} DispatcherEntry;

struct Dispatcher {
    DispatcherEntry entries[DISPATCHER_MAX_ENTRIES];
    size_t count;
};

Dispatcher *dispatcher_create(void) {
    return (Dispatcher *)calloc(1, sizeof(Dispatcher));
}

void dispatcher_destroy(Dispatcher *d) {
    if (d == NULL) return;
    for (size_t i = 0; i < d->count; i++) free(d->entries[i].issuer);
    free(d);
}

int dispatcher_register(Dispatcher *d, const char *issuer, void *verifier_self,
                         VerifierFn verify_fn) {
    if (d == NULL || d->count >= DISPATCHER_MAX_ENTRIES) return -1;
    char *copy = strdup(issuer);
    if (copy == NULL) return -1;
    d->entries[d->count].issuer = copy;
    d->entries[d->count].verifier_self = verifier_self;
    d->entries[d->count].verify_fn = verify_fn;
    d->count++;
    return 0;
}

int dispatcher_verify(Dispatcher *d, const char *authorization_header, Claims **out_claims) {
    if (d == NULL || authorization_header == NULL) return -1;

    const char *prefix = "Bearer ";
    size_t prefix_len = strlen(prefix);
    if (strncmp(authorization_header, prefix, prefix_len) != 0) return -1;
    const char *token = authorization_header + prefix_len;

    JwtParts parts;
    if (jwt_split(token, &parts) != 0) return -1;

    /* 署名検証前に、まずissだけを覗いて担当Verifierを選ぶ(実際の署名/exp/aud検証は
     * 選ばれたverifier自身がtokenを渡されて改めて行う、jwt.cのjwt_claims_valid参照) */
    cJSON *payload = jwt_decode_payload_json(parts.payload_b64, parts.payload_b64_len);
    if (payload == NULL) return -1;

    cJSON *iss = cJSON_GetObjectItemCaseSensitive(payload, "iss");
    if (!cJSON_IsString(iss) || iss->valuestring == NULL) {
        cJSON_Delete(payload);
        return -1;
    }

    VerifierFn fn = NULL;
    void *self = NULL;
    for (size_t i = 0; i < d->count; i++) {
        if (strcmp(d->entries[i].issuer, iss->valuestring) == 0) {
            fn = d->entries[i].verify_fn;
            self = d->entries[i].verifier_self;
            break;
        }
    }
    cJSON_Delete(payload);
    if (fn == NULL) return -1; /* 未知のissuer */

    return fn(self, token, out_claims);
}
