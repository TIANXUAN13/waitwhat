FROM node:20-alpine AS frontend-builder
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend-builder
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/waitwhat-backend .

FROM alpine:3.20
WORKDIR /app

RUN addgroup -S app && adduser -S app -G app
COPY --from=backend-builder /out/waitwhat-backend /app/waitwhat-backend
COPY --from=frontend-builder /src/frontend/dist /app/web
RUN mkdir -p /app/data /app/web && chown -R app:app /app

USER app
EXPOSE 8080
ENV APP_PORT=8080
ENV APP_DATA_DIR=/app/data
ENV APP_WEB_DIR=/app/web

ENTRYPOINT ["/app/waitwhat-backend"]
