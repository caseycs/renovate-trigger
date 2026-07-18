# Build on the native host architecture (BUILDPLATFORM) to avoid QEMU emulation,
# then cross-compile the static Go binary to the requested target arch.
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /renovate-trigger ./cmd/renovate-trigger

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /renovate-trigger /renovate-trigger
USER nonroot:nonroot
ENTRYPOINT ["/renovate-trigger"]
