#include "common/log.h"

#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int g_debug_enabled = 0;

void log_module_init(void) {
    const char *level = getenv("LOG_LEVEL");
    g_debug_enabled = (level != NULL && strcmp(level, "debug") == 0);
}

void log_debugf(const char *fmt, ...) {
    if (!g_debug_enabled) return;
    va_list args;
    va_start(args, fmt);
    vprintf(fmt, args);
    va_end(args);
}
