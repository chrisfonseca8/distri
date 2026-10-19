# syntax=docker/dockerfile:1

ARG GO_VERSION=1.27.2

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/loadgen ./cmd/loadgen \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/local-correctness ./correctnesstesting/local \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/local-loadtest ./loadtesting/local

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app app

WORKDIR /app
COPY --from=build /out/loadgen /out/local-correctness /out/local-loadtest ./

USER app
ENTRYPOINT ["/app/loadgen"]
