# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /out/photochallengebot ./cmd/photochallengebot \
	&& CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /out/importhistory ./cmd/importhistory

FROM alpine:3

RUN adduser -D -H -u 10001 appuser \
	&& mkdir -p /app /data \
	&& chown -R appuser:appuser /app /data

WORKDIR /app

COPY --from=build /out/photochallengebot /usr/local/bin/photochallengebot
COPY --from=build /out/importhistory /usr/local/bin/importhistory
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY migrations ./migrations
COPY templates ./templates

ENV TZ=Europe/Moscow \
	DATABASE_PATH=/data/photochallengebot.sqlite \
	TEMPLATES_DIR=/app/templates

VOLUME ["/data"]

USER appuser

ENTRYPOINT ["photochallengebot"]
