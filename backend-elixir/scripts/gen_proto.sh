#!/bin/sh
# proto/task/v1/task.proto(他言語と1バイトも違わない正本)からElixirコード(protobuf構造体+grpc服務)を
# 生成する。Mixにはビルド時のprotoc自動実行を組み込む標準機能が無いため、他言語のCMake/Gradleの
# add_custom_command/protobufプラグイン相当を、明示的に呼び出すシェルスクリプトとして用意している。
# 生成先(lib/generated/)は.gitignore対象(生成物はコミットしない)
set -e
cd "$(dirname "$0")/.."
export PATH="$PATH:$HOME/.mix/escripts"
rm -rf lib/generated
mkdir -p lib/generated
protoc -I proto -I /opt/homebrew/include --elixir_out=plugins=grpc:./lib/generated proto/task/v1/task.proto
echo "generated: $(find lib/generated -type f)"
