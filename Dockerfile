FROM golang:1.27.2 AS build
ARG VERSION=0.1.0
ARG COMMIT=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT" -o /selectel-s3 ./cmd/selectel-s3
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /selectel-s3 /selectel-s3
USER 65532:65532
ENTRYPOINT ["/selectel-s3"]
CMD ["mcp"]
