# syntax=docker/dockerfile:1

ARG GO_VERSION=1.27.2

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gateway-simulator ./cmd/gateway-simulator

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/gateway-simulator ./
USER app
EXPOSE 9090
ENTRYPOINT ["/app/gateway-simulator"]
