# syntax=docker/dockerfile:1
#
# frontend用ローカル開発イメージ(Vite dev server)
# Production向けの静的ビルド配信
# イメージではない
# 学習用途でホットリロードを優先し、srcをvolumeマウントして使う前提
# (docker-compose.yaml参照)
FROM node:22-bookworm-slim

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm install

COPY . .

EXPOSE 5173

# --host: コンテナ外(ブラウザ)からアクセスできるよう 0.0.0.0 で listen する
CMD ["npm", "run", "dev", "--", "--host"]
