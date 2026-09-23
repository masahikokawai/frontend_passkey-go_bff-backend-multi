#include "auth/user_resolver.h"

#include <stdbool.h>
#include <stdlib.h>

#include "auth/jwt.h"
#include "repository/task_repository.h"

/*
 * backend(Go)のresolveUserID・backend-rustのsrc/auth/mod.rs::resolve_user_id・
 * backend-cppのResolveUserIdFromAuthHeaderと同じ分岐:
 *   - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの → usersをidで検索
 *   - Keycloak発行のJWT: subはkeycloak_sub → user_keycloaks経由でusersを検索
 * どちらも見つからなければTASK_ERR_UNAUTHORIZED(401 unauthenticated /
 * gRPC UNAUTHENTICATEDに対応する、他言語の「user not provisioned」相当)
 */
TaskError auth_resolve_user_id(Dispatcher *dispatcher, const char *authorization_header,
                                int64_t *out_user_id) {
    if (authorization_header == NULL) return TASK_ERR_UNAUTHORIZED;

    Claims *claims = NULL;
    if (dispatcher_verify(dispatcher, authorization_header, &claims) != 0) {
        return TASK_ERR_UNAUTHORIZED;
    }

    TaskError result = TASK_ERR_UNAUTHORIZED;
    if (auth_is_local_issuer(claims->iss)) {
        char *endptr = NULL;
        long long id = strtoll(claims->sub, &endptr, 10);
        if (endptr != claims->sub && *endptr == '\0') {
            bool found = false;
            if (task_repository_find_user_by_id(id, &found) == TASK_OK && found) {
                *out_user_id = (int64_t)id;
                result = TASK_OK;
            }
        }
    } else {
        int64_t user_id = 0;
        bool found = false;
        if (task_repository_find_user_by_keycloak_sub(claims->sub, &user_id, &found) == TASK_OK &&
            found) {
            *out_user_id = user_id;
            result = TASK_OK;
        }
    }

    claims_destroy(claims);
    return result;
}
