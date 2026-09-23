#ifndef TESTS_TEST_TOKEN_HELPER_H
#define TESTS_TEST_TOKEN_HELPER_H

#include <openssl/evp.h>

/*
 * テスト専用の署名済みJWT組み立てヘルパー。本番コード(src/auth/)はJWTのdecode/検証のみ
 * 行い、署名生成ロジックは一切持たない(誰にも署名させる必要が無いため)。ここでは
 * テストが「実際に検証可能な本物のJWT」を用意するために、署名生成だけをテスト側で
 * 自作している(backend-rustのtests内make_hmac_tokenと同じ役割)
 */

/* HS256で署名したJWTを組み立てる(呼び出し側がfree()すること)。
 * exp = time(NULL) + exp_offset_secs (負の値を渡すと期限切れトークンを作れる) */
char *make_hmac_token(const char *secret, const char *iss, const char *aud, const char *sub,
                       long long exp_offset_secs);

/* make_hmac_tokenにazpクレームを追加できる版(make_rsa_token_with_azpのHMAC版。
 * external_auth_test.cの「ローカルHMAC発行トークンは、azpが正しくても外部公開APIでは
 * 拒否される」テスト用。azp==NULLまたは空文字列ならクレーム自体を省略する。
 * 呼び出し側がfree()すること) */
char *make_hmac_token_with_azp(const char *secret, const char *iss, const char *aud,
                                const char *sub, const char *azp, long long exp_offset_secs);

/* RS256で署名したJWTを組み立てる(呼び出し側がfree()すること)。private_keyはRSA鍵の
 * EVP_PKEY*。headerに"kid"クレームを含める(jwks_test.cのモックJWKSサーバー用) */
char *make_rsa_token(EVP_PKEY *private_key, const char *kid, const char *iss, const char *aud,
                      const char *sub, long long exp_offset_secs);

/* make_rsa_tokenにazpクレームを追加できる版(外部公開APIのRequireExternalClientAuth相当の
 * テスト用、azp==NULLまたは空文字列ならクレーム自体を省略する。呼び出し側がfree()すること) */
char *make_rsa_token_with_azp(EVP_PKEY *private_key, const char *kid, const char *iss,
                               const char *aud, const char *sub, const char *azp,
                               long long exp_offset_secs);

#endif /* TESTS_TEST_TOKEN_HELPER_H */
