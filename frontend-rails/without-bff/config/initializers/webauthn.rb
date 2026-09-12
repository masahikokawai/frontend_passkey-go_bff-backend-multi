# CONTRACT.mdセクション22.9: パスキー(WebAuthn)のRelying Party設定。
# rp_id="localhost"はbff(Go)・bff-rails側と揃え、同一ブラウザで登録したパスキーを
# どのアプリからでも使い回せるようにする。
#
# WebAuthn.configureのブロックは即座に評価されるため、Zeitwerkのオートロードがまだ
# 準備できていない初期化タイミングでAppConfigを参照するとNameError(uninitialized constant)に
# なる(bff-rails/config/initializers/webauthn.rbと同じ既知の回避策)。明示的にrequireする。
require_relative "../../app/services/app_config"

WebAuthn.configure do |config|
  config.rp_id = AppConfig.webauthn_rp_id
  config.rp_name = AppConfig.webauthn_rp_name
  config.allowed_origins = [AppConfig.webauthn_origin]
end
