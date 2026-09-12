# UserResolver は検証済みJWT Claimsから内部ユーザーを解決する
#
# backend/internal/handler/v1/task.go の resolveUserID と同じロジック
# (CONTRACT.mdセクション16.5): ローカル発行(HMAC/RSA)issのJWTはsubに内部user_idが
# そのまま入っており、Keycloak発行のJWTはsubがkeycloak_subなので users.keycloak_sub で引く
#
# どちらも見つからなければ UserNotProvisioned(REST: 403 user_not_provisioned、
# gRPC: PERMISSION_DENIED "user not provisioned")
class UserResolver
  class UserNotProvisioned < StandardError; end

  def self.resolve(claims)
    if claims.local_issuer?
      id = Integer(claims.subject, exception: false)
      raise UserNotProvisioned, "ローカル発行issのsubが数値ではない: #{claims.subject.inspect}" if id.nil?

      User.find_by(id: id) || raise(UserNotProvisioned, "user_id=#{id} が見つからない")
    else
      User.find_by(keycloak_sub: claims.subject) ||
        raise(UserNotProvisioned, "keycloak_sub=#{claims.subject} が見つからない")
    end
  end
end
