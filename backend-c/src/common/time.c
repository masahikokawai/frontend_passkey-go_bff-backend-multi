#include "common/time.h"

#include <ctype.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

void task_today_utc_iso(char *out_buf, size_t out_buf_len) {
    time_t tt = time(NULL);
    struct tm utc_tm;
    gmtime_r(&tt, &utc_tm); /* gmtime_r: UTC基準(localtime_rは絶対に使わない) */
    snprintf(out_buf, out_buf_len, "%04d-%02d-%02d", utc_tm.tm_year + 1900, utc_tm.tm_mon + 1,
              utc_tm.tm_mday);
}

void task_now_mysql_datetime(char *out_buf, size_t out_buf_len) {
    time_t tt = time(NULL);
    struct tm utc_tm;
    gmtime_r(&tt, &utc_tm);
    snprintf(out_buf, out_buf_len, "%04d-%02d-%02d %02d:%02d:%02d", utc_tm.tm_year + 1900,
              utc_tm.tm_mon + 1, utc_tm.tm_mday, utc_tm.tm_hour, utc_tm.tm_min, utc_tm.tm_sec);
}

int task_is_valid_iso_date(const char *s) {
    if (s == NULL || strlen(s) != 10 || s[4] != '-' || s[7] != '-') return 0;
    const int digit_positions[] = {0, 1, 2, 3, 5, 6, 8, 9};
    for (int i = 0; i < 8; i++) {
        if (!isdigit((unsigned char)s[digit_positions[i]])) return 0;
    }
    int year = atoi((char[]){s[0], s[1], s[2], s[3], 0});
    int month = atoi((char[]){s[5], s[6], 0});
    int day = atoi((char[]){s[8], s[9], 0});
    if (month < 1 || month > 12 || day < 1) return 0;
    static const int days_in_month[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
    int max_day = days_in_month[month - 1];
    int leap = (year % 4 == 0 && year % 100 != 0) || (year % 400 == 0);
    if (month == 2 && leap) max_day = 29;
    return day <= max_day;
}

size_t task_utf8_codepoint_length(const char *s) {
    size_t count = 0;
    size_t i = 0;
    size_t len = strlen(s);
    while (i < len) {
        unsigned char c = (unsigned char)s[i];
        if ((c & 0x80) == 0x00) {
            i += 1;
        } else if ((c & 0xE0) == 0xC0) {
            i += 2;
        } else if ((c & 0xF0) == 0xE0) {
            i += 3;
        } else if ((c & 0xF8) == 0xF0) {
            i += 4;
        } else {
            i += 1; /* 不正なバイト列でも無限ループにならないよう1バイトずつ進める */
        }
        count += 1;
    }
    return count;
}
