FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/bot ./cmd/bot \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/manage ./cmd/manage \
 && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/community-feed ./cmd/community-feed \
 && mkdir /out/data
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build --chown=65532:65532 /out/ /app/
USER 65532:65532
ENV DATABASE_PATH=/app/data/concierge.db LISTEN_ADDR=:8080 BOT_LISTEN_ADDR=:8081
EXPOSE 8080 8081
ENTRYPOINT ["/app/api"]
