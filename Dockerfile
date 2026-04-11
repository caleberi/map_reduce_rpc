FROM golang:1.26.1-trixie AS build
WORKDIR /src

ARG TARGETOS=linux
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/map_reduce_rpc ./main.go
RUN mkdir -p /out/plugins && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/wc.so ./plugins/wc/wc.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/indexer.so ./plugins/indexer/indexer.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/jobcount.so ./plugins/jobcount/jobcount.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/mtiming.so ./plugins/mtiming/mtiming.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/rtiming.so ./plugins/rtiming/rtiming.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/crash.so ./plugins/crash/crash.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/nocrash.so ./plugins/nocrash/nocrash.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/early_exit.so ./plugins/early_exit/early_exit.go

FROM debian:bookworm-slim
WORKDIR /app

COPY --from=build /out/map_reduce_rpc /app/map_reduce_rpc
COPY --from=build /out/plugins /app/plugins

ENTRYPOINT ["/app/map_reduce_rpc"]
