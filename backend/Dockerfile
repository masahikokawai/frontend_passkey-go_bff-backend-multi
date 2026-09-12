# syntax=docker/dockerfile:1
#
# backend用マルチステージビルド
# REST v1 + gRPC v2の両方を含む単一バイナリ
# training-go/bff-gin/CONTRACT.md 参照
ARG GO_VERSION=1.27.0

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# CGO_ENABLED=0: 静的リンクしてdistroless/staticで動かす(go-sql-driver/mysqlは純GoなのでCGO不要)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/backend ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/backend /backend

EXPOSE 8080 9090 8081
USER nonroot:nonroot
ENTRYPOINT ["/backend"]
