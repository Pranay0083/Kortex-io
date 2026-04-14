FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kortex .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /out/kortex /usr/local/bin/kortex
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/kortex"]
