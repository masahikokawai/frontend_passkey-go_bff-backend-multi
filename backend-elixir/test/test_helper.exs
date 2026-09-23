# 実DB(docker-compose上のMySQL)を要求するtest/integration/配下のテストは、
# @tag :integration を付けた上でデフォルトのmix testからは除外する
# (backend-java/backend-kotlin/backend-pythonのctest/pytestマーカーの分離と同じ設計)。
# 実行するには `mix test --only integration` を使う(README.md「結合テスト」節参照)
ExUnit.start(exclude: [:integration])
