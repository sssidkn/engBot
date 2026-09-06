FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /usr/local/bin/engbot ./cmd/bot

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /usr/local/bin/engbot /usr/local/bin/engbot
RUN mkdir -p /app/data && chmod 777 /app/data
ENV DATA_DIR=/app/data
ENV DATABASE_PATH=/app/data/engbot.json
ENV LOG_PATH=/app/data/engBot.log
ENV PORT=8080
ENV DEFAULT_TZ=Europe/Moscow
EXPOSE 8080
CMD ["/usr/local/bin/engbot"]
