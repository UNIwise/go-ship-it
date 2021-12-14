FROM golang:1.17-alpine3.13 AS build

WORKDIR /app
COPY ["go.mod", "go.sum", "./"]
RUN go mod download -x
COPY . .
RUN go build -o /build/ship-it main.go

FROM alpine:3.13
ENV GITHUB_APP_ID=86751
ENV GITHUB_CERT_PATH=/keys/key.pem
COPY assets assets
COPY --from=build /build/ship-it /usr/local/bin/ship-it

ENTRYPOINT [ "ship-it", "serve" ]