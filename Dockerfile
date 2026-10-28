FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/ticket-booking ./cmd/booking

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /bin/ticket-booking /app/ticket-booking
COPY migrations ./migrations
COPY docs ./docs

EXPOSE 8081
ENTRYPOINT ["/app/ticket-booking"]
