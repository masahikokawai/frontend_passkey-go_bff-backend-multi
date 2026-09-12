require "webauthn"
require "base64"

# CONTRACT.mdセクション22: パスキー(WebAuthn)登録・ログイン。
# 登録(register/*)は既にKeycloak経由でログイン済みのユーザーが行う追加の認証手段の登録。
# ログイン(login/*)はパスキー自体がKeycloakを経由しない認証手段のため、
# セッション不要の公開エンドポイントにする。
#
# backendの内部API(/internal/v1/auth/webauthn/credentials/*)はbff(Go)と全く同じものを
# 呼ぶため、Reactの既存frontend+bffで登録したパスキーがそのままここでのログインにも使える
# (どちらも同じbackendのwebauthn_credentialsテーブルを参照するため)。
class PasskeysController < ApplicationController
  before_action :require_session!, only: %i[register_begin register_finish]

  # POST /api/auth/passkey/register/begin
  def register_begin
    options = WebAuthn::Credential.options_for_create(
      user: {
        id: current_session.user_id.to_s,
        name: current_session.email.presence || current_session.name.presence || current_session.user_id.to_s
      },
      authenticator_selection: { resident_key: "required", user_verification: "preferred" }
    )
    challenge = Auth::PasskeyChallengeStore.create(challenge: options.challenge, user_id: current_session.user_id)
    render json: { state: challenge.state, options: options }
  end

  # POST /api/auth/passkey/register/finish
  def register_finish
    challenge = Auth::PasskeyChallengeStore.consume(params[:state])
    if challenge.nil? || challenge.user_id.to_s != current_session.user_id.to_s
      render json: { error: "invalid_state" }, status: :unauthorized
      return
    end

    webauthn_credential = WebAuthn::Credential.from_create(credential_params)
    webauthn_credential.verify(challenge.challenge)

    # 【実装時に発見した実際の問題】webauthn gemの`sign_count`は`BinData::Bit32`を
    # 返す(素朴なIntegerではない)。`.to_json`に直接渡すと、BinDataが遅延評価する
    # 長さフィールドの解決に失敗し`NoMethodError: undefined method 'trailing_bytes_length'
    # for an instance of BinData::LazyEvaluator`で落ちる。明示的に`.to_i`で
    # 素のIntegerへ変換してから使うこと(`id`もStringのはずだが念のため`.to_s`で防御的に変換)
    backend = BackendClient.new(current_session.access_token)
    backend.register_passkey!(
      credential_id: webauthn_credential.id.to_s,
      public_key: Base64.strict_encode64(webauthn_credential.public_key),
      sign_count: webauthn_credential.sign_count.to_i,
      transports: credential_params.dig("response", "transports") || [],
      name: params[:name]
    )
    render json: { registered: true }
  rescue WebAuthn::Error => e
    render json: { error: "verification_failed", detail: e.message }, status: :unprocessable_entity
  rescue BackendClient::Error => e
    render json: { error: "backend_error", detail: e.message }, status: :bad_gateway
  end

  # POST /api/auth/passkey/login/begin
  # discoverable credential(resident key)方式のため、allowCredentialsは指定しない
  # (メールアドレス等の事前入力を要求しない、CONTRACT.mdセクション22.2)
  def login_begin
    options = WebAuthn::Credential.options_for_get
    challenge = Auth::PasskeyChallengeStore.create(challenge: options.challenge)
    render json: { state: challenge.state, options: options }
  end

  # POST /api/auth/passkey/login/finish
  def login_finish
    challenge = Auth::PasskeyChallengeStore.consume(params[:state])
    if challenge.nil?
      render json: { error: "invalid_state" }, status: :unauthorized
      return
    end

    webauthn_credential = WebAuthn::Credential.from_get(credential_params)

    backend = WebauthnBackendClient.new
    stored = backend.find_by_credential_id(webauthn_credential.id.to_s)
    if stored.nil?
      render json: { error: "unknown_credential" }, status: :unauthorized
      return
    end

    webauthn_credential.verify(
      challenge.challenge,
      public_key: Base64.decode64(stored["public_key"]),
      sign_count: stored["sign_count"]
    )
    backend.update_sign_count!(webauthn_credential.id.to_s, webauthn_credential.sign_count.to_i)

    # パスキーログインはKeycloakを経由しないため、OIDCのaccess_tokenを持たない。
    # そこでbff(Go)のローカルHMAC認証と同じ自前JWT(Auth::LocalBackendTokenIssuer)を
    # 発行し、それをそのままsessionのaccess_tokenフィールドへ収める。こうすることで
    # TasksController・BackendClient側は「access_tokenが何によって発行されたか」を
    # 一切意識せずに済み、既存コードの変更なしにパスキーログイン後も
    # /api/tasksが動くようになる(backend側のWebauthnBackendClient#find_by_credential_idが
    # 返すname/email/rolesをそのまま使う)
    local_token = Auth::LocalBackendTokenIssuer.issue(
      user_id: stored["user_id"],
      name: stored["name"],
      email: stored["email"],
      roles: stored["roles"]
    )

    session = SessionStore::Session.new(
      user_id: stored["user_id"],
      name: stored["name"],
      email: stored["email"],
      access_token: local_token,
      auth_mode: "passkey"
    )
    issue_session_cookie!(session)
    render json: { logged_in: true, auth_mode: "passkey" }
  rescue WebAuthn::Error => e
    render json: { error: "verification_failed", detail: e.message }, status: :unprocessable_entity
  rescue WebauthnBackendClient::Error => e
    render json: { error: "backend_error", detail: e.message }, status: :bad_gateway
  end

  private

  # WebAuthn::Credential.from_create/from_getは"type"/"id"/"rawId"等の
  # 文字列キーで直接ハッシュアクセスするため、シンボル化してはいけない
  # (実装時に発見: deep_symbolize_keysすると credential["id"] が常にnilになり、
  # 「invalid id」で必ず検証失敗していた)
  def credential_params
    params.require(:credential).permit!.to_h.deep_stringify_keys
  end
end
