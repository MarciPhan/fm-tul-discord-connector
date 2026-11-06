# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/fm-tul-bot ./cmd/bot/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 bot \
 && mkdir /data && chown bot /data
COPY --from=build /out/fm-tul-bot /usr/local/bin/fm-tul-bot
USER bot
# users.json, feeds.json a discord_config.json se čtou/zapisují relativně k WORKDIR
WORKDIR /data
VOLUME /data
EXPOSE 8000
ENTRYPOINT ["fm-tul-bot"]
