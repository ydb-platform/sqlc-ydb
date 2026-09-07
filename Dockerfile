FROM golang:1.26.0-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOMAXPROCS=2 GOGC=20 GOMEMLIMIT=768MiB go build -p 1 -trimpath -ldflags="-s -w" -o /sqlc-ydb ./cmd/sqlc-ydb

FROM scratch
COPY --from=build /sqlc-ydb /sqlc-ydb
WORKDIR /src
ENTRYPOINT ["/sqlc-ydb"]
