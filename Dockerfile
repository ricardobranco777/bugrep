FROM	registry.opensuse.org/opensuse/bci/golang:latest AS build
WORKDIR /src

RUN	zypper addrepo https://download.opensuse.org/repositories/SUSE:/CA/openSUSE_Tumbleweed/SUSE:CA.repo && \
	zypper --gpg-auto-import-keys -n install ca-certificates-suse && \
	update-ca-certificates

COPY	go.mod go.sum ./
RUN	go mod download
COPY	. .

ARG	VERSION=dev
RUN	CGO_ENABLED=0 go build \
	-ldflags "-s -w -X github.com/ricardobranco777/bugrep/internal/cli.Version=${VERSION}" \
	-o /out/bugrep ./cmd/bugrep

FROM	scratch
COPY	--from=build /var/lib/ca-certificates/ca-bundle.pem /etc/ssl/ca-bundle.pem
COPY	--from=build /out/bugrep /usr/bin/bugrep

# No $HOME in scratch, so keep the config under a fixed path.
ENV	XDG_CONFIG_HOME=/config

ENTRYPOINT ["/usr/bin/bugrep"]
