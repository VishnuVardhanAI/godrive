# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS builder
WORKDIR /app
RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o files ./services/files

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/files /files
EXPOSE 50052
ENTRYPOINT ["/files"]