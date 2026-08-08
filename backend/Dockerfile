FROM golang:1.25-alpine AS build
ARG SHARESUB_VERSION
ARG SHARESUB_REVISION
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY VERSION /src/VERSION
COPY backend/ .
RUN test "$(tr -d '\n' < /src/VERSION)" = "$SHARESUB_VERSION"
RUN echo "$SHARESUB_REVISION" | grep -Eq '^[0-9a-f]{40}$'
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/sharesub/sharesub/backend/internal/buildinfo.Version=$SHARESUB_VERSION -X github.com/sharesub/sharesub/backend/internal/buildinfo.Revision=$SHARESUB_REVISION" -o /out/sharesub-api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/sharesub/sharesub/backend/internal/buildinfo.Version=$SHARESUB_VERSION -X github.com/sharesub/sharesub/backend/internal/buildinfo.Revision=$SHARESUB_REVISION" -o /out/sharesub-admin ./cmd/admin

FROM alpine:3.21
RUN addgroup -S sharesub && adduser -S -G sharesub sharesub
COPY --from=build /out/sharesub-api /usr/local/bin/sharesub-api
COPY --from=build /out/sharesub-admin /usr/local/bin/sharesub-admin
USER sharesub
EXPOSE 8080
ENTRYPOINT ["sharesub-api"]
