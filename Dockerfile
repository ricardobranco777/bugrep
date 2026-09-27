# syntax=docker/dockerfile:1

FROM golang:1.27 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X github.com/ricardobranco777/bugrep/internal/cli.Version=${VERSION}" \
    -o /out/bugrep \
    ./cmd/bugrep

# distroless:nonroot brings CA certificates (needed for HTTPS to every
# tracker) and a non-root user, with no shell and no package manager.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bugrep /usr/bin/bugrep
USER nonroot:nonroot
ENTRYPOINT ["/usr/bin/bugrep"]
