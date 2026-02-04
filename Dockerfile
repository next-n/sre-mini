FROM golang:1.23-alpine AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /bin/sre-mini ./main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=build /bin/sre-mini /sre-mini

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/sre-mini"]
