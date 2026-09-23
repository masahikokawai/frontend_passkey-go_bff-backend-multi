package com.bffgin.backend.auth;

import com.bffgin.backend.domain.TaskError;
import com.bffgin.backend.domain.User;
import com.bffgin.backend.repository.TaskRepository;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.sql.SQLException;
import java.util.Optional;

/**
 * REST/gRPCの両トランスポートが共有するuser_id解決ロジック(認証ロジックを複製しない設計)。
 * backend(Go)のresolveUserID・backend-rustのauth::resolve_user_idと同じ分岐:
 *   - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの → usersをidで検索
 *   - Keycloak発行のJWT: subはkeycloak_sub → user_keycloaks経由で検索
 * どちらも見つからなければUserNotProvisioned
 */
public final class UserResolver {

    private static final Logger log = LoggerFactory.getLogger(UserResolver.class);

    private final Dispatcher dispatcher;
    private final TaskRepository repository;

    public UserResolver(Dispatcher dispatcher, TaskRepository repository) {
        this.dispatcher = dispatcher;
        this.repository = repository;
    }

    public long resolve(String authorizationHeader) throws TaskError {
        if (authorizationHeader == null || !authorizationHeader.startsWith("Bearer ")) {
            throw TaskError.unauthorized();
        }
        String token = authorizationHeader.substring("Bearer ".length());

        Claims claims;
        try {
            claims = dispatcher.verify(token);
        } catch (VerifyException e) {
            throw TaskError.unauthorized();
        }

        try {
            if (Dispatcher.isLocalIssuer(claims.iss())) {
                long id;
                try {
                    id = Long.parseLong(claims.sub());
                } catch (NumberFormatException e) {
                    throw TaskError.userNotProvisioned();
                }
                Optional<User> user = repository.findUserById(id);
                if (user.isEmpty()) {
                    throw TaskError.userNotProvisioned();
                }
                log.debug("user_id resolved via local issuer: iss={} sub={} user_id={}", claims.iss(), claims.sub(), user.get().id());
                return user.get().id();
            }

            Optional<User> user = repository.findUserByKeycloakSub(claims.sub());
            if (user.isEmpty()) {
                throw TaskError.userNotProvisioned();
            }
            log.debug("user_id resolved via keycloak_sub: iss={} sub={} user_id={}", claims.iss(), claims.sub(), user.get().id());
            return user.get().id();
        } catch (SQLException e) {
            throw TaskError.internal(e.getMessage());
        }
    }
}
