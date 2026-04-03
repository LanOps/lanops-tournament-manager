FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o thournament ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S thournament && adduser -S thournament -G thournament

WORKDIR /app
COPY --from=builder /app/thournament .

USER thournament
EXPOSE 8080
ENTRYPOINT ["./thournament"]
