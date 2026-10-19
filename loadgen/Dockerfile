# syntax=docker/dockerfile:1

ARG GO_VERSION=1.27.2

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/loadgen ./cmd/loadgen

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app app

WORKDIR /app
COPY --from=build /out/loadgen ./loadgen

USER app
ENTRYPOINT ["/app/loadgen"]
