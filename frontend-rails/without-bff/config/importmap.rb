# CONTRACT.mdセクション22.9: パスキー登録/ログインの儀式(fetch + navigator.credentials呼び出し)を
# app/javascript/配下のESモジュールとして実装し、ブラウザへはimportmap-rails経由で配信する
# (frontend-rails/with-bffと同じ構成。webauthn_codec.jsはnode --testでも単体テストできるよう
# 依存性注入・DOM操作無しの純粋関数として切り出している)
pin "webauthn_codec"
pin "passkey_client"
