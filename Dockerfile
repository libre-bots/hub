FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/hub ./cmd/hub

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hub /hub
EXPOSE 50051
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --start-interval=1s CMD ["/hub", "health"]
ENTRYPOINT ["/hub"]
