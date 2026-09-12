require "webauthn"
require "base64"

# CONTRACT.mdセクション22.9: frontend-rails/without-bffへのパスキー追加。
# bff-rails/app/controllers/passkeys_controller.rbと同じ設計・同じ内部API契約(backendの
# webauthn_credentialsテーブルを共有)だが、セッションの持ち方が異なる
# (bff-railsはRedisセッション、こちらはRailsのCookieセッション+session[:user]ハッシュ)。
#
# 登録(register/*)は既にKeycloakログイン済みのユーザーが行う追加の認証手段の登録。
# ログイン(login/*)はパスキー自体がKeycloakを経由しない認証手段のため、セッション不要の公開エンドポイント。
class PasskeysController < ApplicationController
  before_action :require_login_json, only: %i[register_begin register_finish]

  # POST /auth/passkey/register/begin
  def register_begin
    user = session[:user]
    options = WebAuthn::Credential.options_for_create(
      user: {
        id: user["user_id"].to_s,
        name: user["email"].presence || user["name"].presence || user["user_id"].to_s
      },
      authenticator_selection: { resident_key: "required", user_verification: "preferred" }
    )
    challenge = Auth::PasskeyChallengeStore.create(challenge: options.challenge, user_id: user["user_id"])
    render json: { state: challenge.state, options: options }
  end

  # POST /auth/passkey/register/finish
  def register_finish
    challenge = Auth::PasskeyChallengeStore.consume(params[:state])
    if challenge.nil? || challenge.user_id.to_s != session[:user]["user_id"].to_s
      render json: { error: "invalid_state" }, status: :unauthorized
      return
    end

    webauthn_credential = WebAuthn::Credential.from_create(credential_params)
    webauthn_credential.verify(challenge.challenge)

    # 【bff-rails実装時に発見した実際の問題を踏襲】webauthn gemの`sign_count`は
    # `BinData::Bit32`を返す(素朴なIntegerではない)ため、明示的に`.to_i`で変換する
    #
    # 【CONTRACT.mdセクション22.8の実機バグを踏まえ、最初からBE/BSフラグを正しく配線する】
    # authenticator_dataのcredential_backup_eligible?/credential_backup_state?を
    # 登録時にそのまま保存しないと、go-webauthn側(bffがこのcredentialでログインする場合)が
    # 「登録時と申告内容が矛盾している」としてクラウド同期パスキーのログインを常に拒否してしまう
    authenticator_data = webauthn_credential.response.authenticator_data
    backend = BackendClient.new(session[:access_token])
    backend.register_passkey!(
      credential_id: webauthn_credential.id.to_s,
      public_key: Base64.strict_encode64(webauthn_credential.public_key),
      sign_count: webauthn_credential.sign_count.to_i,
      backup_eligible: authenticator_data.credential_backup_eligible?,
      # 【実装時に発見】webauthn gemのメソッド名は`credential_backed_up?`であり
      # `credential_backup_state?`という名前のメソッドは存在しない(gem 3.4.3のソースで確認)
      backup_state: authenticator_data.credential_backed_up?,
      transports: credential_params.dig("response", "transports") || [],
      name: params[:name]
    )
    render json: { registered: true }
  rescue WebAuthn::Error => e
    render json: { error: "verification_failed", detail: e.message }, status: :unprocessable_entity
  rescue BackendClient::Error => e
    render json: { error: "backend_error", detail: e.message }, status: :bad_gateway
  end

  # POST /auth/passkey/login/begin
  # discoverable credential(resident key)方式のため、allowCredentialsは指定しない
  def login_begin
    options = WebAuthn::Credential.options_for_get
    challenge = Auth::PasskeyChallengeStore.create(challenge: options.challenge)
    render json: { state: challenge.state, options: options }
  end

  # POST /auth/passkey/login/finish
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

    # パスキーログインはKeycloakを経由しないためOIDCのaccess_tokenを持たない。
    # bff(Go)/bff-railsと同じ自前JWT(Auth::LocalBackendTokenIssuer)を発行し、
    # そのままsession[:access_token]へ収める(backendへの以後の呼び出しに使い回す設計)
    local_token = Auth::LocalBackendTokenIssuer.issue(
      user_id: stored["user_id"],
      name: stored["name"],
      email: stored["email"],
      roles: stored["roles"]
    )

    # 【Session Fixation対策、既存のomniauth_callback/handle_manual_callbackと同じ理由】
    # ログイン成立の瞬間にRailsのセッションCookie自体を新しいIDへ差し替える
    reset_session
    session[:user] = {
      "sub" => nil,
      "user_id" => stored["user_id"],
      "name" => stored["name"],
      "email" => stored["email"]
    }
    session[:access_token] = local_token
    session[:auth_mode] = "passkey"
    render json: { logged_in: true, auth_mode: "passkey" }
  rescue WebAuthn::Error => e
    render json: { error: "verification_failed", detail: e.message }, status: :unprocessable_entity
  rescue WebauthnBackendClient::Error => e
    render json: { error: "backend_error", detail: e.message }, status: :bad_gateway
  end

  private

  # WebAuthn::Credential.from_create/from_getは"type"/"id"/"rawId"等の文字列キーで
  # 直接ハッシュアクセスするため、シンボル化してはいけない
  # (bff-rails実装時に発見: deep_symbolize_keysするとcredential["id"]が常にnilになり、
  # 「invalid id」で必ず検証失敗していた)
  def credential_params
    params.require(:credential).permit!.to_h.deep_stringify_keys
  end

  def require_login_json
    return if session[:user].present? && session[:access_token].present?

    render json: { error: "unauthorized" }, status: :unauthorized
  end
end
