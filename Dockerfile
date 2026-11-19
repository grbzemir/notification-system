# syntax=docker/dockerfile:1

# Tek Dockerfile üç servisi de derler: docker build --build-arg SERVICE=api|worker|mockprovider
FROM golang:1.27-alpine AS build
WORKDIR /src

# Önce yalnızca bağımlılık dosyaları: kod değişince bağımlılık katmanı yeniden indirilmez.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG SERVICE
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

# Çalışma imajı: küçük, root olmayan kullanıcı. alpine'in wget'i healthcheck için yeterli.
FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
