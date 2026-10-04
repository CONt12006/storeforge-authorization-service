FROM golang:1.23-alpine AS build
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /authorization-service ./cmd/authorization-service

FROM alpine:3.20
RUN addgroup -S app && adduser -S app -G app
COPY --from=build /authorization-service /usr/local/bin/authorization-service
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/authorization-service"]
