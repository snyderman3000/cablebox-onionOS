#!/bin/sh
# Builds the OnionOS package: dist/Cablebox-v<version>-OnionOS.zip
set -e
cd "$(dirname "$0")"
VERSION=$(grep -o 'const version = "[^"]*"' main.go | cut -d'"' -f2)
[ -f go.work ] || go mod tidy
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w" -o package/Cablebox/cablebox .
rm -rf dist && mkdir -p dist/stage/App
cp -r package/Cablebox dist/stage/App/
rm -rf dist/stage/App/Cablebox/data
(cd dist/stage && zip -qr "../Cablebox-v$VERSION-OnionOS.zip" App)
rm -rf dist/stage
echo "Built dist/Cablebox-v$VERSION-OnionOS.zip"
