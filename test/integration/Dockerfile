# Image used by integration_test.go to run the tests that change the host
# network (links, routes, iptables and ebtables) inside a throwaway container.
ARG GO_VERSION
FROM golang:${GO_VERSION}

RUN apt-get update \
    && apt-get install -y --no-install-recommends iptables ebtables \
    && rm -rf /var/lib/apt/lists/*
