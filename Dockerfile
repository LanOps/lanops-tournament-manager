FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o lanops-tournament-manager ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S lanops && adduser -S lanops -G lanops

WORKDIR /app
COPY --from=builder /app/lanops-tournament-manager .

USER lanops
EXPOSE 8080
ENTRYPOINT ["./lanops-tournament-manager"]
