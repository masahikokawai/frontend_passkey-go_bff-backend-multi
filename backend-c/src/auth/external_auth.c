#include "auth/external_auth.h"

#include <string.h>

#include "auth/jwt.h"

TaskError auth_require_external_client(Dispatcher *dispatcher, const char *authorization_header,
                                        const char *external_api_client_id) {
    if (authorization_header == NULL) return TASK_ERR_UNAUTHORIZED;

    Claims *claims = NULL;
    if (dispatcher_verify(dispatcher, authorization_header, &claims) != 0) {
        return TASK_ERR_UNAUTHORIZED;
    }

    /* ローカルHMAC/RSA発行のJWTは内部REST/gRPC専用であり、署名検証自体は正しく通っても
     * 外部公開APIでは受け付けない(Keycloak発行のみ許可、CONTRACT.mdセクション11) */
    TaskError result = TASK_ERR_UNAUTHORIZED;
    if (!auth_is_local_issuer(claims->iss) &&
        strcmp(claims->azp, external_api_client_id) == 0) {
        result = TASK_OK;
    }

    claims_destroy(claims);
    return result;
}
