FROM golang:1.23-alpine AS builder
WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/waitwhat-backend .

FROM alpine:3.20
WORKDIR /app

RUN addgroup -S app && adduser -S app -G app
COPY --from=builder /out/waitwhat-backend /app/waitwhat-backend
RUN mkdir -p /app/data && chown -R app:app /app

USER app
EXPOSE 8080
ENV APP_PORT=8080
ENV APP_DATA_DIR=/app/data

ENTRYPOINT ["/app/waitwhat-backend"]
