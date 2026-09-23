#ifndef AUTH_BASE64URL_H
#define AUTH_BASE64URL_H

#include <stddef.h>
#include <stdint.h>

/*
 * JWTで使うbase64url(パディング無し、'+'/'/'の代わりに'-'/'_'を使う変種)のエンコード/
 * デコード。backend-cpp/src/auth/base64.hppのBase64UrlDecodeと同じテーブル方式をCへ
 * 移植したもの(decodeのみ本番コードで使用、encodeはテストで署名済みトークンを
 * 組み立てるために提供している)
 */

/* 戻り値: malloc確保したバイト列(呼び出し側がfree()すること)。*out_lenに実際のバイト数を
 * 書き込む。不正な文字は読み飛ばす(パディング文字'='も含む、標準的なbase64url実装の挙動) */
uint8_t *base64url_decode(const char *in, size_t in_len, size_t *out_len);

/* 戻り値: malloc確保したNUL終端文字列(呼び出し側がfree()すること)。失敗時(malloc失敗)はNULL */
char *base64url_encode(const uint8_t *in, size_t in_len);

#endif /* AUTH_BASE64URL_H */
