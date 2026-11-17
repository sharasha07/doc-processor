FROM golang:1.27-bookworm AS build

RUN apt-get update && apt-get install -y gcc g++ && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=1
ENV CC=gcc
ENV CXX=g++
ENV CGO_LDFLAGS="-L/app/native/linux_amd64 -lextractous_ffi"

RUN go build -o /bin/api ./cmd/api

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y zlib1g ca-certificates tesseract-ocr tesseract-ocr-eng fontconfig fonts-dejavu-core && rm -rf /var/lib/apt/lists/*

COPY --from=build /bin/api /app/api
COPY native/linux_amd64 /app/native/linux_amd64
COPY templates /app/templates

ENV LD_LIBRARY_PATH=/app/native/linux_amd64
ENV TEMPLATES_DIR=/app/templates

WORKDIR /app

EXPOSE 8080

ENTRYPOINT ["/app/api"]
