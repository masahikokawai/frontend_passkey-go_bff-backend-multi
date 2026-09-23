#include "auth/jwks_verifier.h"

#include <cjson/cJSON.h>
#include <curl/curl.h>
#include <openssl/bn.h>
#include <openssl/core_names.h>
#include <openssl/evp.h>
#include <openssl/param_build.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

#include "auth/base64url.h"
#include "common/log.h"

/* --- libcurlでJWKS URLをGETする、成長バッファ --- */

typedef struct {
    char *data;
    size_t len;
    size_t cap;
} CurlBuffer;

static size_t curl_write_cb(void *ptr, size_t size, size_t nmemb, void *userdata) {
    size_t add = size * nmemb;
    CurlBuffer *buf = (CurlBuffer *)userdata;
    if (buf->len + add + 1 > buf->cap) {
        size_t new_cap = (buf->len + add + 1) * 2;
        /*
         * 【メモリ管理のバッド/グッドプラクティス】reallocの戻り値を元のポインタへ
         * 直接代入すると、失敗時(NULLが返る)にそれまで受信していたデータを指す
         * ポインタ自体を失ってしまい、解放できなくなる(CWE-401、解放漏れ):
         *
         *   buf->data = realloc(buf->data, new_cap);  // ← 失敗時にbuf->dataがNULLに
         *                                              //   上書きされ、元のバッファがリークする
         *   if (buf->data == NULL) return 0;
         *
         * 修正後: 一時変数で受け取り、成功を確認してから初めてbuf->dataへ反映する
         * (失敗時は元のbuf->dataがそのまま残るため、呼び出し元が正しくfree()できる) */
        char *grown = (char *)realloc(buf->data, new_cap);
        if (grown == NULL) return 0; /* 0を返すとcurl側が転送をエラー終了させる */
        buf->data = grown;
        buf->cap = new_cap;
    }
    memcpy(buf->data + buf->len, ptr, add);
    buf->len += add;
    buf->data[buf->len] = '\0';
    return add;
}

/* 戻り値: malloc確保したレスポンスボディ(呼び出し側がfree()すること)。失敗時NULL */
static char *http_get(const char *url) {
    CURL *curl = curl_easy_init();
    if (curl == NULL) return NULL;

    CurlBuffer buf;
    buf.cap = 256;
    buf.len = 0;
    buf.data = (char *)malloc(buf.cap);
    if (buf.data == NULL) {
        curl_easy_cleanup(curl);
        return NULL;
    }
    buf.data[0] = '\0';

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, curl_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &buf);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT, 5L);
    curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 1L);

    CURLcode res = curl_easy_perform(curl);
    long http_code = 0;
    curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &http_code);
    curl_easy_cleanup(curl);

    if (res != CURLE_OK || http_code != 200) {
        free(buf.data);
        return NULL;
    }
    return buf.data;
}

/* --- kid -> (n, e)キャッシュ --- */

typedef struct {
    char *kid;
    uint8_t *n;
    size_t n_len;
    uint8_t *e;
    size_t e_len;
} JwksKeyEntry;

struct JwksVerifier {
    char *jwks_url;
    char *issuer;
    char *audience;
    pthread_mutex_t mutex;
    JwksKeyEntry *keys;
    size_t key_count;
};

static void free_keys(JwksKeyEntry *keys, size_t count) {
    for (size_t i = 0; i < count; i++) {
        free(keys[i].kid);
        free(keys[i].n);
        free(keys[i].e);
    }
    free(keys);
}

JwksVerifier *jwks_verifier_create(const char *jwks_url, const char *issuer,
                                    const char *audience) {
    JwksVerifier *v = (JwksVerifier *)calloc(1, sizeof(JwksVerifier));
    if (v == NULL) return NULL;
    v->jwks_url = strdup(jwks_url);
    v->issuer = strdup(issuer);
    v->audience = strdup(audience);
    if (v->jwks_url == NULL || v->issuer == NULL || v->audience == NULL) {
        jwks_verifier_destroy(v);
        return NULL;
    }
    pthread_mutex_init(&v->mutex, NULL);
    return v;
}

void jwks_verifier_destroy(JwksVerifier *v) {
    if (v == NULL) return;
    free(v->jwks_url);
    free(v->issuer);
    free(v->audience);
    free_keys(v->keys, v->key_count);
    pthread_mutex_destroy(&v->mutex);
    free(v);
}

/* JWKSを取得し、RSA/sig用の鍵だけを抽出してキャッシュを丸ごと入れ替える
 * (backend-cppのJwksVerifier::Refreshと同じ「取得できたら全置き換え」方式) */
static int refresh(JwksVerifier *v) {
    log_debugf("auth debug: jwks refresh triggered url=%s\n", v->jwks_url);
    char *body = http_get(v->jwks_url);
    if (body == NULL) return -1;

    cJSON *root = cJSON_Parse(body);
    free(body);
    if (root == NULL) return -1;

    cJSON *keys_json = cJSON_GetObjectItemCaseSensitive(root, "keys");
    if (!cJSON_IsArray(keys_json)) {
        cJSON_Delete(root);
        return -1;
    }

    JwksKeyEntry *new_keys = NULL;
    size_t new_count = 0;
    cJSON *item = NULL;
    cJSON_ArrayForEach(item, keys_json) {
        cJSON *kty = cJSON_GetObjectItemCaseSensitive(item, "kty");
        if (!cJSON_IsString(kty) || strcmp(kty->valuestring, "RSA") != 0) continue;
        cJSON *use = cJSON_GetObjectItemCaseSensitive(item, "use");
        if (cJSON_IsString(use) && strcmp(use->valuestring, "sig") != 0) continue;
        cJSON *kid = cJSON_GetObjectItemCaseSensitive(item, "kid");
        cJSON *n = cJSON_GetObjectItemCaseSensitive(item, "n");
        cJSON *e = cJSON_GetObjectItemCaseSensitive(item, "e");
        if (!cJSON_IsString(kid) || !cJSON_IsString(n) || !cJSON_IsString(e)) continue;

        size_t n_len = 0, e_len = 0;
        uint8_t *n_bytes = base64url_decode(n->valuestring, strlen(n->valuestring), &n_len);
        uint8_t *e_bytes = base64url_decode(e->valuestring, strlen(e->valuestring), &e_len);
        char *kid_copy = strdup(kid->valuestring);
        if (n_bytes == NULL || e_bytes == NULL || kid_copy == NULL) {
            free(n_bytes);
            free(e_bytes);
            free(kid_copy);
            continue;
        }

        JwksKeyEntry *grown =
            (JwksKeyEntry *)realloc(new_keys, sizeof(JwksKeyEntry) * (new_count + 1));
        if (grown == NULL) {
            free(n_bytes);
            free(e_bytes);
            free(kid_copy);
            continue;
        }
        new_keys = grown;
        new_keys[new_count].kid = kid_copy;
        new_keys[new_count].n = n_bytes;
        new_keys[new_count].n_len = n_len;
        new_keys[new_count].e = e_bytes;
        new_keys[new_count].e_len = e_len;
        new_count++;
    }
    cJSON_Delete(root);

    pthread_mutex_lock(&v->mutex);
    JwksKeyEntry *old_keys = v->keys;
    size_t old_count = v->key_count;
    v->keys = new_keys;
    v->key_count = new_count;
    pthread_mutex_unlock(&v->mutex);
    free_keys(old_keys, old_count);
    return 0;
}

/* ロック保持中にn/eをコピーして返す(OpenSSLでの鍵構築というロックを保持したまま
 * 行いたくない重い処理を、ロック解放後に行うため。backend-cppがpair<vector,vector>を
 * コピーしてからロックを抜けるのと同じ設計) */
static int find_key_copy(JwksVerifier *v, const char *kid, uint8_t **out_n, size_t *out_n_len,
                          uint8_t **out_e, size_t *out_e_len) {
    int found = 0;
    pthread_mutex_lock(&v->mutex);
    for (size_t i = 0; i < v->key_count; i++) {
        if (strcmp(v->keys[i].kid, kid) != 0) continue;
        uint8_t *n_copy = (uint8_t *)malloc(v->keys[i].n_len > 0 ? v->keys[i].n_len : 1);
        uint8_t *e_copy = (uint8_t *)malloc(v->keys[i].e_len > 0 ? v->keys[i].e_len : 1);
        if (n_copy != NULL && e_copy != NULL) {
            memcpy(n_copy, v->keys[i].n, v->keys[i].n_len);
            memcpy(e_copy, v->keys[i].e, v->keys[i].e_len);
            *out_n = n_copy;
            *out_n_len = v->keys[i].n_len;
            *out_e = e_copy;
            *out_e_len = v->keys[i].e_len;
            found = 1;
        } else {
            free(n_copy);
            free(e_copy);
        }
        break;
    }
    pthread_mutex_unlock(&v->mutex);
    return found;
}

/* n(modulus)・e(exponent)の生バイト列からRSA公開鍵のEVP_PKEYを組み立てる(OpenSSL 3 API、
 * backend-cpp/src/auth/jwt.cppのBuildRsaPublicKeyと同じ手順) */
static EVP_PKEY *build_rsa_public_key(const uint8_t *n, size_t n_len, const uint8_t *e,
                                       size_t e_len) {
    BIGNUM *bn_n = BN_bin2bn(n, (int)n_len, NULL);
    BIGNUM *bn_e = BN_bin2bn(e, (int)e_len, NULL);
    EVP_PKEY *pkey = NULL;

    if (bn_n != NULL && bn_e != NULL) {
        OSSL_PARAM_BLD *bld = OSSL_PARAM_BLD_new();
        if (bld != NULL) {
            OSSL_PARAM_BLD_push_BN(bld, OSSL_PKEY_PARAM_RSA_N, bn_n);
            OSSL_PARAM_BLD_push_BN(bld, OSSL_PKEY_PARAM_RSA_E, bn_e);
            OSSL_PARAM *params = OSSL_PARAM_BLD_to_param(bld);
            EVP_PKEY_CTX *ctx = EVP_PKEY_CTX_new_from_name(NULL, "RSA", NULL);
            if (ctx != NULL && params != NULL && EVP_PKEY_fromdata_init(ctx) > 0) {
                EVP_PKEY_fromdata(ctx, &pkey, EVP_PKEY_PUBLIC_KEY, params);
            }
            if (ctx != NULL) EVP_PKEY_CTX_free(ctx);
            if (params != NULL) OSSL_PARAM_free(params);
            OSSL_PARAM_BLD_free(bld);
        }
    }

    /*
     * 【メモリ管理のバッド/グッドプラクティス】BN_bin2bnで確保したBIGNUMは、
     * OSSL_PARAM_BLD_push_BNに渡した後もこちら(呼び出し側)の所有物のままである
     * (OSSL_PARAM_BLD_push_BNは値をコピーするだけで所有権を奪わない、というより
     * 正確にはOSSL_PARAMが内部でBIGNUMの表現を借用するため、以下のように早期に
     * 解放するとOSSL_PARAM_BLD_to_param/EVP_PKEY_fromdataの内部処理がまだ参照している
     * 可能性がありuse-after-freeになりうる:
     *
     *   OSSL_PARAM_BLD_push_BN(bld, OSSL_PKEY_PARAM_RSA_N, bn_n);
     *   BN_free(bn_n);  // ← pushした直後に解放してしまう(所有権はまだ移っていない)
     *   OSSL_PARAM_BLD_push_BN(bld, OSSL_PKEY_PARAM_RSA_E, bn_e);
     *
     * 修正後: OSSL_PARAM_BLD_to_param/EVP_PKEY_fromdataが完全に完了した後、
     * 関数の最後でまとめてBN_free()する */
    if (bn_n != NULL) BN_free(bn_n);
    if (bn_e != NULL) BN_free(bn_e);
    return pkey;
}

static int verify_rs256(EVP_PKEY *pkey, const char *signing_input, size_t signing_input_len,
                         const uint8_t *signature, size_t signature_len) {
    EVP_MD_CTX *ctx = EVP_MD_CTX_new();
    if (ctx == NULL) return 0;
    int ok = 0;
    if (EVP_DigestVerifyInit(ctx, NULL, EVP_sha256(), NULL, pkey) == 1) {
        ok = EVP_DigestVerify(ctx, signature, signature_len,
                               (const unsigned char *)signing_input, signing_input_len) == 1;
    }
    EVP_MD_CTX_free(ctx);
    return ok;
}

int jwks_verifier_verify(void *self, const char *token, Claims **out_claims) {
    JwksVerifier *v = (JwksVerifier *)self;

    JwtParts parts;
    if (jwt_split(token, &parts) != 0) return -1;

    /* アルゴリズムホワイトリストの理由はhmac_verifier.cのコメント参照。
     * このVerifierはRS256専用であり、ヘッダのalgがそれ以外なら即座に拒否する */
    char *alg = jwt_decode_header_alg(parts.header_b64, parts.header_b64_len);
    int alg_ok = (alg != NULL && strcmp(alg, "RS256") == 0);
    free(alg);
    if (!alg_ok) return -1;

    char *kid = jwt_decode_header_kid(parts.header_b64, parts.header_b64_len);
    if (kid == NULL) return -1;

    cJSON *payload = jwt_decode_payload_json(parts.payload_b64, parts.payload_b64_len);
    if (payload == NULL) {
        free(kid);
        return -1;
    }
    if (!jwt_claims_valid(payload, v->issuer, v->audience)) {
        cJSON_Delete(payload);
        free(kid);
        return -1;
    }

    uint8_t *n = NULL, *e = NULL;
    size_t n_len = 0, e_len = 0;
    if (!find_key_copy(v, kid, &n, &n_len, &e, &e_len)) {
        /* kid不一致 → 一度だけ再取得する(backend-rust/backend-cppと同じ戦略) */
        if (refresh(v) != 0 || !find_key_copy(v, kid, &n, &n_len, &e, &e_len)) {
            free(kid);
            cJSON_Delete(payload);
            return -1;
        }
    }
    free(kid);

    EVP_PKEY *pkey = build_rsa_public_key(n, n_len, e, e_len);
    free(n);
    free(e);
    if (pkey == NULL) {
        cJSON_Delete(payload);
        return -1;
    }

    size_t sig_len = 0;
    uint8_t *sig = base64url_decode(parts.signature_b64, parts.signature_b64_len, &sig_len);
    if (sig == NULL) {
        EVP_PKEY_free(pkey);
        cJSON_Delete(payload);
        return -1;
    }

    int ok = verify_rs256(pkey, parts.signing_input, parts.signing_input_len, sig, sig_len);
    free(sig);

    /*
     * 【メモリ管理のバッド/グッドプラクティス】以下のように成功時のパスにしか
     * EVP_PKEY_free()を置かないと、署名不一致(okがfalse)のたびに早期returnして
     * EVP_PKEY(RSA公開鍵の内部表現、内部にBIGNUM等を保持する比較的重いオブジェクト)が
     * リークする(CWE-401)。検証失敗自体は攻撃者が送る不正なトークンで容易に起こせるため、
     * このリークは外部から連続してトリガーされ得る:
     *
     *   if (!ok) { cJSON_Delete(payload); return -1; }  // ← EVP_PKEY_free(pkey)を忘れている
     *   EVP_PKEY_free(pkey);
     *   ...
     *
     * 修正後: 成功/失敗どちらのパスでも、以降で使い終わったらすぐEVP_PKEY_free(pkey)を
     * 呼んでから分岐する */
    EVP_PKEY_free(pkey);
    if (!ok) {
        cJSON_Delete(payload);
        return -1;
    }

    Claims *claims = jwt_claims_from_json(payload);
    cJSON_Delete(payload);
    if (claims == NULL) return -1;
    *out_claims = claims;
    return 0;
}
