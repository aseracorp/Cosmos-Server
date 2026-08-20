#!/bin/bash

echo " ---- Build Cosmos ----"

rm -rf build

cp src/update.go src/launcher/update.go

env GOARCH=arm CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o build/cosmos-arm src/*.go
if [ $? -ne 0 ]; then
    exit 1
fi
env GOARCH=arm CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o build/cosmos-launcher-arm ./src/launcher/launcher.go ./src/launcher/update.go
if [ $? -ne 0 ]; then
    exit 1
fi

env GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o build/cosmos-arm64 src/*.go
if [ $? -ne 0 ]; then
    exit 1
fi
env GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o build/cosmos-launcher-arm64 ./src/launcher/launcher.go ./src/launcher/update.go
if [ $? -ne 0 ]; then
    exit 1
fi

env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o build/cosmos src/*.go
if [ $? -ne 0 ]; then
    exit 1
fi
env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o build/cosmos-launcher ./src/launcher/launcher.go ./src/launcher/update.go
if [ $? -ne 0 ]; then
    exit 1
fi

# Compress the executable (test performance impact before using in production).
upx -9 build/cosmos-arm
upx -9 build/cosmos-launcher-arm
upx -9 build/cosmos-arm64
upx -9 build/cosmos-launcher-arm64
upx -9 build/cosmos
upx -9 build/cosmos-launcher

echo " ---- Build complete, copy assets ----"

cp start.sh build/start.sh
cp restic-arm restic-arm64 restic build/

chmod +x build/start.sh
chmod +x build/cosmos
chmod +x build/cosmos-arm
chmod +x build/cosmos-arm64
chmod +x build/cosmos-launcher
chmod +x build/cosmos-launcher-arm
chmod +x build/cosmos-launcher-arm64
chmod +x build/restic
chmod +x build/restic-arm
chmod +x build/restic-arm64

cp -r static build/
cp -r GeoLite2-Country.mmdb build/
cp nebula-arm-cert nebula-arm64-cert nebula-cert nebula-arm nebula-arm64 nebula build/
cp -r Logo.png build/
mkdir build/images
cp client/src/assets/images/icons/cosmos_gray.png build/cosmos_gray.png
cp client/src/assets/images/icons/cosmos_gray.png cosmos_gray.png
echo '{' > build/meta.json
cat package.json | grep -E '"version"' >> build/meta.json
echo '  "buildDate": "'`date`'",' >> build/meta.json
echo '  "built from": "'`hostname`'"' >> build/meta.json
echo '}' >> build/meta.json

echo " ---- copy complete ----"