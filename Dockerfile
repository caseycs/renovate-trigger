FROM golang:1.24-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /renovate-trigger ./cmd/renovate-trigger

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /renovate-trigger /renovate-trigger
USER nonroot:nonroot
ENTRYPOINT ["/renovate-trigger"]
