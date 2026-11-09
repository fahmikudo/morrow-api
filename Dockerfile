FROM golang:1.26-bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server \
    && CGO_ENABLED=0 go build -trimpath -o bin/migrate ./cmd/migrate

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /app/bin ./bin
COPY migrations ./migrations
EXPOSE 8080
CMD ["sh", "-c", "./bin/migrate up && exec ./bin/server"]
