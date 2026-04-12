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
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/early_exit.so ./plugins/early_exit/early_exit.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/bigram.so ./plugins/bigram/bigram.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/charfreq.so ./plugins/charfreq/charfreq.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/emailextract.so ./plugins/emailextract/emailextract.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/linestats.so ./plugins/linestats/linestats.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/sentiment.so ./plugins/sentiment/sentiment.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/topwords.so ./plugins/topwords/topwords.go && \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildmode=plugin -o /out/plugins/urlextractor.so ./plugins/urlextractor/urlextractor.go

FROM debian:bookworm-slim
WORKDIR /app

COPY --from=build /out/map_reduce_rpc /app/map_reduce_rpc
COPY --from=build /out/plugins /app/plugins

ENTRYPOINT ["/app/map_reduce_rpc"]
