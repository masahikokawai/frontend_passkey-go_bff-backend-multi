#include "auth/hmac_verifier.h"

#include <cjson/cJSON.h>
#include <openssl/crypto.h>
#include <openssl/hmac.h>
#include <stdlib.h>
#include <string.h>

#include "auth/base64url.h"

struct HmacVerifier {
    char *secret;
    char *issuer;
    char *audience;
};

HmacVerifier *hmac_verifier_create(const char *secret, const char *issuer, const char *audience) {
    HmacVerifier *v = (HmacVerifier *)calloc(1, sizeof(HmacVerifier));
    if (v == NULL) return NULL;
    v->secret = strdup(secret);
    v->issuer = strdup(issuer);
    v->audience = strdup(audience);
    if (v->secret == NULL || v->issuer == NULL || v->audience == NULL) {
        hmac_verifier_destroy(v);
        return NULL;
    }
    return v;
}

void hmac_verifier_destroy(HmacVerifier *v) {
    if (v == NULL) return;
    free(v->secret);
    free(v->issuer);
    free(v->audience);
    free(v);
}

int hmac_verifier_verify(void *self, const char *token, Claims **out_claims) {
    const HmacVerifier *v = (const HmacVerifier *)self;

    JwtParts parts;
    if (jwt_split(token, &parts) != 0) return -1;

    /*
     * 【セキュリティ上のバッド/グッドプラクティス】JWTヘッダのalg(署名アルゴリズム)は
     * 攻撃者が自由に書き換えられる値であり、これを鵜呑みにして検証方式を選ぶ実装は
     * アルゴリズム混同攻撃(algorithm confusion、実際のJWT実装で繰り返し報告されてきた
     * 既知の脆弱性クラス)を許してしまう。例えば以下のように「ヘッダのalgを見てから
     * 分岐する」実装だと、攻撃者が"alg":"none"を指定して署名検証そのものを
     * 省略させたり、RS256用の公開鍵をHS256の共有シークレットとして誤用させたりできる:
     *
     *   char *alg = jwt_decode_header_alg(parts.header_b64, parts.header_b64_len);
     *   if (strcmp(alg, "none") == 0) { *out_claims = jwt_claims_from_json(payload); return 0; }
     *
     * 修正後: このVerifier自身が担当するアルゴリズム(HS256)をコード側で固定し、
     * ヘッダのalgがそれと完全一致しない限り無条件に拒否する(ヘッダの値へ検証ロジックの
     * 分岐そのものを委ねない) */
    char *alg = jwt_decode_header_alg(parts.header_b64, parts.header_b64_len);
    int alg_ok = (alg != NULL && strcmp(alg, "HS256") == 0);
    free(alg);
    if (!alg_ok) return -1;

    cJSON *payload = jwt_decode_payload_json(parts.payload_b64, parts.payload_b64_len);
    if (payload == NULL) return -1;
    if (!jwt_claims_valid(payload, v->issuer, v->audience)) {
        cJSON_Delete(payload);
        return -1;
    }

    unsigned char mac[EVP_MAX_MD_SIZE];
    unsigned int mac_len = 0;
    HMAC(EVP_sha256(), v->secret, (int)strlen(v->secret),
         (const unsigned char *)parts.signing_input, parts.signing_input_len, mac, &mac_len);

    size_t sig_len = 0;
    uint8_t *sig = base64url_decode(parts.signature_b64, parts.signature_b64_len, &sig_len);
    if (sig == NULL) {
        cJSON_Delete(payload);
        return -1;
    }

    /*
     * 【セキュリティ上のバッド/グッドプラクティス】MAC(メッセージ認証コード)の比較を
     * 通常のmemcmp/strcmpで行うと、多くの実装は不一致が見つかった時点で即座に返るため、
     * 「何バイト目まで一致していたか」が比較にかかった時間差として外部から観測できて
     * しまう(タイミングサイドチャネル攻撃、CWE-208)。これを悪用すると攻撃者は
     * 1バイトずつ総当たりで正しい署名を推測できる:
     *
     *   int equal = (sig_len == mac_len) && memcmp(mac, sig, mac_len) == 0;  // ← 不一致位置で早期リターンしうる
     *
     * 修正後: OpenSSLのCRYPTO_memcmpは常に全バイトを比較してから結果を返す定数時間比較
     * 関数であり、一致/不一致にかかる時間が入力(不一致位置)に依存しない
     * (長さの比較自体は秘密情報ではないため、先に行っても安全) */
    int equal = (sig_len == mac_len) && CRYPTO_memcmp(mac, sig, mac_len) == 0;
    free(sig);
    if (!equal) {
        cJSON_Delete(payload);
        return -1;
    }

    Claims *claims = jwt_claims_from_json(payload);
    cJSON_Delete(payload);
    if (claims == NULL) return -1;
    *out_claims = claims;
    return 0;
}
