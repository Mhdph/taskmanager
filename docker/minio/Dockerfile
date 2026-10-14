FROM golang:1.24.8-bookworm AS build

# Pin the official source so rebuilding uses the same MinIO version.
ARG MINIO_COMMIT=7aac2a2c5b7c882e68c1ce017d8256be2feea27f
ADD https://github.com/minio/minio/archive/${MINIO_COMMIT}.tar.gz /tmp/minio.tar.gz
WORKDIR /src
RUN tar -xzf /tmp/minio.tar.gz --strip-components=1 -C /src \
    && CGO_ENABLED=0 go build -trimpath -o /out/minio .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/minio /usr/local/bin/minio
COPY --from=build /src/LICENSE /usr/share/doc/minio/LICENSE
EXPOSE 9000 9001
ENTRYPOINT ["minio"]
CMD ["server", "/data", "--console-address", ":9001"]
