FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /binbash .

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /binbash /app/binbash
ENV BINBASH_DB_PATH=/data/binbash.db
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/binbash"]
