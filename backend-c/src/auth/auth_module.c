#include "auth/auth_module.h"

#include <stddef.h>
#include <stdio.h>
#include <string.h>

#include "auth/hmac_verifier.h"
#include "auth/jwks_verifier.h"
#include "auth/jwt.h"

static Dispatcher *g_dispatcher = NULL;
static HmacVerifier *g_hmac_verifier = NULL;
static JwksVerifier *g_local_rsa_verifier = NULL;
static JwksVerifier *g_keycloak_verifier = NULL;
static char g_external_api_client_id[256] = "";

int auth_module_init(const char *keycloak_issuer, const char *keycloak_jwks_url,
                      const char *expected_audience, const char *local_hmac_secret,
                      const char *local_rsa_jwks_url, const char *external_api_client_id) {
    snprintf(g_external_api_client_id, sizeof(g_external_api_client_id), "%s",
             external_api_client_id != NULL ? external_api_client_id : "");
    g_dispatcher = dispatcher_create();
    g_hmac_verifier =
        hmac_verifier_create(local_hmac_secret, AUTH_LOCAL_HMAC_ISSUER, expected_audience);
    g_local_rsa_verifier =
        jwks_verifier_create(local_rsa_jwks_url, AUTH_LOCAL_RSA_ISSUER, expected_audience);
    g_keycloak_verifier =
        jwks_verifier_create(keycloak_jwks_url, keycloak_issuer, expected_audience);

    if (g_dispatcher == NULL || g_hmac_verifier == NULL || g_local_rsa_verifier == NULL ||
        g_keycloak_verifier == NULL) {
        return -1;
    }

    dispatcher_register(g_dispatcher, AUTH_LOCAL_HMAC_ISSUER, g_hmac_verifier,
                         hmac_verifier_verify);
    dispatcher_register(g_dispatcher, AUTH_LOCAL_RSA_ISSUER, g_local_rsa_verifier,
                         jwks_verifier_verify);
    dispatcher_register(g_dispatcher, keycloak_issuer, g_keycloak_verifier, jwks_verifier_verify);
    return 0;
}

Dispatcher *auth_module_dispatcher(void) { return g_dispatcher; }

const char *auth_module_external_api_client_id(void) { return g_external_api_client_id; }
