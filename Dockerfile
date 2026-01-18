# === Flutter build stage ===
FROM cirrusci/flutter:stable AS flutter-build

WORKDIR /app
COPY ui/ ./
RUN flutter build web --release

# === Go build stage ===
FROM golang:1.22-alpine AS go-build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=flutter-build /app/build/web ./internal/ui/dist

RUN CGO_ENABLED=0 GOOS=linux go build -o monalias ./cmd/monalias

# === Runtime stage ===
FROM alpine:3.19

RUN adduser -D -H -u 10001 monalias
WORKDIR /app

COPY --from=go-build /app/monalias /app/monalias

RUN mkdir -p /data && chown monalias:monalias /data

USER monalias

ENV MONALIAS_DB_PATH=/data/monalias.db

EXPOSE 80
EXPOSE 8080

ENTRYPOINT ["/app/monalias"]
