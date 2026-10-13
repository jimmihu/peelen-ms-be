# syntax=docker/dockerfile:1
# Samakan versi Go dengan go.mod
ARG GO_VERSION=1.21

FROM golang:${GO_VERSION}-alpine AS base
WORKDIR /src
# Dependency sudah di-vendor, jadi tidak perlu go mod download
ENV GOFLAGS=-mod=vendor
COPY . .

# Dipanggil Jenkins: docker build --target test .
FROM base AS test
RUN go vet ./... && go test ./...

FROM base AS builder
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bootstrap-admin ./cmd/bootstrap-admin

# Stage terakhir = image yang dijalankan
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/server /out/bootstrap-admin /
EXPOSE 8080
USER nonroot:nonroot
# Exec form: binary jadi PID 1 dan menerima SIGTERM (graceful shutdown)
ENTRYPOINT ["/server"]
