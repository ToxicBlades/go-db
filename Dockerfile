FROM golang:1.22 AS build

WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -o /go-db ./cmd/go-db

FROM gcr.io/distroless/static-debian12

WORKDIR /app
COPY --from=build /go-db /go-db
COPY seed.sql /app/seed.sql

VOLUME ["/data"]
EXPOSE 5433
ENTRYPOINT ["/go-db", "server"]
CMD ["--db", "/data/go-db.db", "--addr", ":5433", "--seed", "/app/seed.sql"]
