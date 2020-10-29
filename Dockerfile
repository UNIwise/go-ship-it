FROM golang:1.15-alpine3.12 AS build

WORKDIR /app
COPY ["go.mod", "go.sum", "./"]
RUN go mod download -x
COPY . .
RUN go build -o /build/server server.go

FROM alpine:3.12
ENV GITHUB_APP_ID=86751
ENV GITHUB_CERT_PATH=/keys/key.pem
COPY --from=build /build/server /usr/local/bin/server

ENTRYPOINT [ "server" ]