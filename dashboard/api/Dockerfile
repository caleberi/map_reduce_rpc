FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/dashboard-api .

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /out/dashboard-api /app/dashboard-api
EXPOSE 4400
ENTRYPOINT ["/app/dashboard-api"]
