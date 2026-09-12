# 【テスト監査で追加】importmap-rails gemはGemfileに既に入っていたが、実際には config/importmap.rb が無く配線されていなかった
# (app/views/home/index.html.erbの<script>に全ロジックがインラインで書かれ、テストも一切無かった)
# base64url ⇔ ArrayBuffer 変換(app/javascript/webauthn_codec.js)を Node 組み込みのテストランナーで単体テストできるようにするため、
# ブラウザ側もこのファイルを実装の二重管理にならないよう同じソースから ESモジュールとして import する
pin "webauthn_codec"
pin "passkey_client"
