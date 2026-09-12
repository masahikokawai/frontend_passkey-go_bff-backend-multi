#!/usr/bin/env bash
# gateway/nginx の起動スクリプト: nginx本体 + サイドカー(backend.task-languageのポーリング)の両方を立ち上げる
# CONTRACT.mdセクション20.8参照
#
# gateway/go(`cd gateway/go && go run .`)とはどちらか一方だけを起動すること
# (どちらも:8081をbindするため同時起動は不可)
set -euo pipefail

cd "$(dirname "$0")"

if ! command -v nginx >/dev/null 2>&1; then
  echo "nginxが見つかりません。'brew install nginx' でインストールしてください" >&2
  exit 1
fi

mkdir -p run logs

echo "nginxを検証します(nginx -t)..."
nginx -p "$(pwd)" -c nginx.conf -t

echo "nginxを起動します(:8081)..."
nginx -p "$(pwd)" -c nginx.conf

cleanup() {
  echo "nginxを停止します..."
  nginx -p "$(pwd)" -c nginx.conf -s stop 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "サイドカー(backend.task-languageのポーリング)を起動します..."
cd sidecar
go run .
