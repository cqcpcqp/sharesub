FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sharesub-api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sharesub-admin ./cmd/admin

FROM alpine:3.21
RUN addgroup -S sharesub && adduser -S -G sharesub sharesub
COPY --from=build /out/sharesub-api /usr/local/bin/sharesub-api
COPY --from=build /out/sharesub-admin /usr/local/bin/sharesub-admin
USER sharesub
EXPOSE 8080
ENTRYPOINT ["sharesub-api"]
