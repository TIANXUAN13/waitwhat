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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/waitwhat-backend .
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --chown=65532:65532 --from=backend-builder /out/waitwhat-backend /app/waitwhat-backend
COPY --chown=65532:65532 --from=frontend-builder /src/frontend/dist /app/web
COPY --chown=65532:65532 --from=backend-builder /out/data /app/data

EXPOSE 8080
ENV APP_PORT=8080
ENV APP_DATA_DIR=/app/data
ENV APP_WEB_DIR=/app/web

ENTRYPOINT ["/app/waitwhat-backend"]
