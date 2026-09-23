#include "auth/base64url.h"

#include <stdlib.h>

/* backend-cpp/src/auth/base64.hppのkTableと同一のASCII→6bit値テーブル
 * ('-'=62, '_'=63、標準base64の'+'/'/'をURL安全な文字に置き換えたもの) */
static const int8_t kTable[256] = {
    // clang-format off
    -1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,
    -1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,62,-1,62,-1,63,52,53,54,55,56,57,58,59,60,61,-1,-1,-1,-1,-1,-1,
    -1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,-1,-1,-1,-1,63,
    -1,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,-1,-1,-1,-1,-1,
    -1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,
    -1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,
    -1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,
    -1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,
    // clang-format on
};

static const char kAlphabet[65] =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";

/*
 * 【メモリ管理のバッド/グッドプラクティス】base64は4文字→3バイトのため出力は入力より
 * 必ず小さくなるが、以下のように「出力バイト数を呼び出し側へ返さず、malloc時に使った
 * in_lenをそのまま出力長として扱う」設計だと、パディング文字や不正文字を読み飛ばした分
 * だけ実際のデコード結果より長い「ゴミを含む」バッファとして扱われてしまう(意味的な
 * 境界外読み取り):
 *
 *   uint8_t *out = malloc(in_len);
 *   ...decode処理...
 *   return out;  // ← 呼び出し側はin_lenバイト全部を有効なデータとして読んでしまう
 *
 * 修正後: 実際にデコードできたバイト数を*out_lenへ書き戻し、呼び出し側は必ず*out_len
 * を経由してから読む(mallocサイズ自体はin_len確保のままで構わない、4文字→3バイト以下に
 * しかならないため出力が入力長を超えることはなく、そちらは安全) */
uint8_t *base64url_decode(const char *in, size_t in_len, size_t *out_len) {
    uint8_t *out = (uint8_t *)malloc(in_len > 0 ? in_len : 1);
    if (out == NULL) return NULL;

    /*
     * 【メモリ管理ではなく未定義動作(UB)のバッド/グッドプラクティス】valの型をint
     * (符号付き)にしたまま、抽出済みの上位ビットをマスクせず永遠に左シフトし続けると、
     * 入力が数文字を超えたあたりでシフト結果が符号付きintの表現範囲(32bit)を超え、
     * 符号付き整数のオーバーフロー(C標準上の未定義動作、AddressSanitizer/
     * UndefinedBehaviorSanitizerで実際に検出される)になる:
     *
     *   int val = 0;
     *   ...
     *   val = (val << 6) | d;  // ← valを一度も縮小しないため、ループが進むほど
     *                          //   上位ビットが伸び続け、いずれ符号ビットまで破壊する
     *
     * 修正後: 型をuint32_t(符号無し、シフトのオーバーフローが未定義動作にならない)に
     * 変更した上で、バイトを1つ取り出すたびにvalを「まだ使っていない下位bitsビットだけ」に
     * マスクして縮小する(valが指数的に伸び続けるのを防ぎ、シフト量も常に安全な範囲に保つ) */
    size_t out_pos = 0;
    uint32_t val = 0;
    int bits = 0;
    for (size_t i = 0; i < in_len; i++) {
        unsigned char c = (unsigned char)in[i];
        int8_t d = kTable[c];
        if (d < 0) continue; /* '='等のパディング/不正文字は読み飛ばす */
        val = (val << 6) | (uint32_t)d;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            out[out_pos++] = (uint8_t)((val >> bits) & 0xFF);
            val &= (1u << bits) - 1u;
        }
    }
    *out_len = out_pos;
    return out;
}

char *base64url_encode(const uint8_t *in, size_t in_len) {
    size_t out_len = ((in_len + 2) / 3) * 4;
    char *out = (char *)malloc(out_len + 1);
    if (out == NULL) return NULL;

    size_t oi = 0;
    size_t i = 0;
    for (; i + 3 <= in_len; i += 3) {
        uint32_t n = ((uint32_t)in[i] << 16) | ((uint32_t)in[i + 1] << 8) | (uint32_t)in[i + 2];
        out[oi++] = kAlphabet[(n >> 18) & 0x3F];
        out[oi++] = kAlphabet[(n >> 12) & 0x3F];
        out[oi++] = kAlphabet[(n >> 6) & 0x3F];
        out[oi++] = kAlphabet[n & 0x3F];
    }
    size_t rem = in_len - i;
    if (rem == 1) {
        uint32_t n = (uint32_t)in[i] << 16;
        out[oi++] = kAlphabet[(n >> 18) & 0x3F];
        out[oi++] = kAlphabet[(n >> 12) & 0x3F];
    } else if (rem == 2) {
        uint32_t n = ((uint32_t)in[i] << 16) | ((uint32_t)in[i + 1] << 8);
        out[oi++] = kAlphabet[(n >> 18) & 0x3F];
        out[oi++] = kAlphabet[(n >> 12) & 0x3F];
        out[oi++] = kAlphabet[(n >> 6) & 0x3F];
    }
    out[oi] = '\0';
    return out;
}
