FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
COPY cmd ./cmd

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/nubererum ./cmd/nubererum

# things-cli from the original SDK, pinned to a release, plus a small patch for
# what Things' lists still need (see docs/DEPLOYMENT.md, "Patched Things CLI").
ARG THINGS_SDK_VERSION=v0.6.0
ARG THINGS_SDK_COMMIT=6919edc62da7ac98dbd8afbdffccc48fcd0807ed
COPY patches ./patches
RUN apk add --no-cache git \
  && git clone --quiet --depth 1 --branch "$THINGS_SDK_VERSION" https://github.com/arthursoares/things-cloud-sdk.git /tmp/things-cloud-sdk \
  && test "$(git -C /tmp/things-cloud-sdk rev-parse HEAD)" = "$THINGS_SDK_COMMIT" \
  && git -C /tmp/things-cloud-sdk apply /src/patches/things-cloud-sdk-arthursoares.patch \
  && cd /tmp/things-cloud-sdk \
  && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/things-cli ./cmd/things-cli

FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata \
  && adduser -D -H -u 10001 app \
  && mkdir -p /app /data \
  && chown -R app:app /app /data

WORKDIR /app

COPY --from=build /out/nubererum /usr/local/bin/nubererum
COPY --from=build /out/things-cli /usr/local/bin/things-cli
COPY public ./public

USER app

ENV HOST=0.0.0.0
ENV PORT=4173
ENV DATA_DIR=/data
ENV THINGS_CLI_CACHE=/data/things-cli-state-v8.json

EXPOSE 4173

ENTRYPOINT ["/usr/local/bin/nubererum"]
