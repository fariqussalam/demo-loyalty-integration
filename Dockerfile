FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/loyalty-demo .

FROM alpine:3.23

RUN addgroup -S app && adduser -S app -G app

COPY --from=build --chown=app:app /out/loyalty-demo /usr/local/bin/loyalty-demo
RUN mkdir /data && chown app:app /data

USER app
WORKDIR /data
VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["loyalty-demo"]
