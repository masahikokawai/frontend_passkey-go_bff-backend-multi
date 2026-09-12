# CONTRACT.mdセクション22: パスキー(WebAuthn)のRelying Party設定。
# rp_id="localhost"はbff(Go)側と揃え、同一ブラウザで両BFFから同じパスキーを使い回せるようにする。
#
# 【実装時に発見した実際の問題】WebAuthn.configureのブロックは(Rack::Corsの設定DSLと違い)
# 即座に評価されるため、Zeitwerkのオートロードがまだ準備できていない初期化タイミングで
# AppConfigを参照すると`NameError: uninitialized constant AppConfig`になる。
# 明示的にrequireして回避する(既存の`app/middleware`系初期化子と同じ既知の回避策)。
require_relative "../../app/services/app_config"

WebAuthn.configure do |config|
  config.rp_id = AppConfig.webauthn_rp_id
  config.rp_name = AppConfig.webauthn_rp_name
  config.allowed_origins = [AppConfig.webauthn_origin]
end
