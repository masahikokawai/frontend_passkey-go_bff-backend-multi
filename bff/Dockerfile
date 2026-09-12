# syntax=docker/dockerfile:1

# bff-gin/bff の本番相当イメージ
# ローカル開発は `mise exec go@1.27.0 -- go run ./cmd/server`
# でも動かせるが、docker composeでは常にこのイメージを使う

ARG GO_VERSION=1.27.0

FROM golang:${GO_VERSION}-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# CGO_ENABLED=0: 静的リンクしてdistroless/staticで動かす
# BFFが直接DBを触ることはない(GORMを使うのはbackend側のみ)ため、CGO非依存
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/bff ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/bff /bff
COPY --from=build /src/internal/featureflag/flags.yaml /internal/featureflag/flags.yaml

EXPOSE 8080

USER nonroot:nonroot

ENV FEATURE_FLAG_FILE_PATH=/internal/featureflag/flags.yaml

ENTRYPOINT ["/bff"]
