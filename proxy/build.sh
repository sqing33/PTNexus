#!/bin/bash

echo "Building proxy..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o pt-nexus-box-proxy .
echo "Build complete: proxy"
