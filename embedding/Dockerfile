FROM golang:1.26-bookworm AS builder

WORKDIR /src

ARG TARGETARCH
ARG ONNX_VERSION=1.17.1

RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
	&& ARCH="$TARGETARCH" \
	&& if [ "$ARCH" = "amd64" ]; then ONNX_ARCH=x64; \
	   elif [ "$ARCH" = "arm64" ]; then ONNX_ARCH=aarch64; \
	   else echo "unsupported arch: $ARCH" && exit 1; fi \
	&& curl -fsSL -o /tmp/onnx.tgz \
		"https://github.com/microsoft/onnxruntime/releases/download/v${ONNX_VERSION}/onnxruntime-linux-${ONNX_ARCH}-${ONNX_VERSION}.tgz" \
	&& mkdir -p /opt/onnxruntime \
	&& tar -xzf /tmp/onnx.tgz -C /opt/onnxruntime --strip-components=1 \
	&& rm -rf /var/lib/apt/lists/* /tmp/onnx.tgz

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /out/embedding .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libgomp1 \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=builder /opt/onnxruntime /opt/onnxruntime
COPY --from=builder /out/embedding /usr/local/bin/embedding

ENV ONNX_PATH=/opt/onnxruntime/lib/libonnxruntime.so
ENV CACHE_DIR=/models
ENV ADDR=:8081
ENV LD_LIBRARY_PATH=/opt/onnxruntime/lib

VOLUME ["/models"]
EXPOSE 8081

CMD ["embedding"]
