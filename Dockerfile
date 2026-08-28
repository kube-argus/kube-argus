# One Dockerfile, three images via --target: operator | broker | proxy.
# Build from the repository ROOT (service module replaces ../operator).
#
#   docker build --target operator -t <repo>/kargus-operator:tag .
#   docker build --target broker   -t <repo>/kargus-broker:tag   .
#   docker build --target proxy    -t <repo>/kargus-proxy:tag    .
#
# Per-module `go mod download` caches deps so source changes don't re-download.
FROM golang:1.27 AS gobase
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH}
WORKDIR /src

# --- operator binary ---
FROM gobase AS operator-build
COPY operator/go.mod operator/go.sum ./operator/
RUN cd operator && go mod download
COPY operator/ ./operator/
RUN cd operator && go build -trimpath -o /out/manager ./cmd

# --- service binaries (broker + proxy); needs operator via the local replace ---
FROM gobase AS service-build
COPY operator/go.mod operator/go.sum ./operator/
COPY service/go.mod service/go.sum ./service/
RUN cd service && go mod download
COPY operator/ ./operator/
COPY service/ ./service/
RUN cd service && go build -trimpath -o /out/broker ./cmd/server \
 && go build -trimpath -o /out/proxy ./cmd/proxy

# --- images ---
FROM gcr.io/distroless/static:nonroot AS operator
COPY --from=operator-build /out/manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]

FROM gcr.io/distroless/static:nonroot AS broker
COPY --from=service-build /out/broker /broker
COPY --from=service-build /out/proxy /proxy
USER 65532:65532
ENTRYPOINT ["/broker"]

FROM gcr.io/distroless/static:nonroot AS proxy
COPY --from=service-build /out/proxy /proxy
USER 65532:65532
ENTRYPOINT ["/proxy"]
