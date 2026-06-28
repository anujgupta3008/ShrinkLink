# Stage 1: Build
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o main cmd/api/main.go

# Stage 2: Run
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/main .

EXPOSE 8080

CMD ["./main"]
