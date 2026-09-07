FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/family-calendar-bot ./cmd/bot

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/family-calendar-bot /app/bot
WORKDIR /app
VOLUME ["/app/data"]
USER nonroot:nonroot
ENTRYPOINT ["/app/bot"]
