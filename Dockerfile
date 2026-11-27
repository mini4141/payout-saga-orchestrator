# One image holds every service binary; compose and ECS pick one with `command`.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN for c in api worker relay receiver ops-ui mocks migrate; do \
      CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/$c ./cmd/$c || exit 1; \
    done

FROM alpine:3.22
RUN apk add --no-cache ca-certificates curl && adduser -D -u 10001 app
COPY --from=build /out/ /usr/local/bin/
USER app
