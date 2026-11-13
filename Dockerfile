FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/MXSniffer ./cmd/mxsniffer

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S -G app app
COPY --from=build /out/MXSniffer /usr/local/bin/MXSniffer
USER app
EXPOSE 1314
ENTRYPOINT ["MXSniffer"]
