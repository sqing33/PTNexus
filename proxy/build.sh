#!/bin/bash

echo "Building proxy..."
CGO_ENABLED=0 GOOS="${GOOS:-linux}" GOARCH="${GOARCH:-amd64}" go build -buildvcs=false -ldflags="-s -w" -o pt-nexus-box-proxy .
echo "Build complete: proxy"
