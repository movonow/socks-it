#!/bin/bash

SCRIPT_DIR=$(dirname "$(realpath "$0")")
OUTPUT_DIR=$(realpath "$SCRIPT_DIR/../bin")
mkdir -p $OUTPUT_DIR

APPLICATIONS=(
  "client"
  "server"
)

PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "windows/arm64"
)

for APP in "${APPLICATIONS[@]}"; do
  for PLATFORM in "${PLATFORMS[@]}"; do
    # Split the platform string into OS and ARCH
    OS=$(echo $PLATFORM | cut -d'/' -f1)
    ARCH=$(echo $PLATFORM | cut -d'/' -f2)

    # Set the output binary name (add .exe for Windows)
    OUTPUT_NAME="${APP}-${OS}-${ARCH}"
    if [ "$OS" == "windows" ]; then
      OUTPUT_NAME="$OUTPUT_NAME.exe"
    fi

    echo "Building $APP for $OS/$ARCH..."
    CGO_ENABLED=0 GOOS=$OS GOARCH=$ARCH go build -o "$OUTPUT_DIR/$OUTPUT_NAME" "socks.it/proxy/bin/$APP"

    if [ $? -eq 0 ]; then
      echo "Successfully built: $OUTPUT_DIR/$OUTPUT_NAME"
    else
      echo "Failed to build $APP for $OS/$ARCH" >&2
    fi
  done
done

OUTPUT_NAME="generateSuite"
PACKAGE_NAME="socks.it/nothing/bin"
echo "Building $OUTPUT_NAME for local machine..."
go build -o "$OUTPUT_DIR/$OUTPUT_NAME" $PACKAGE_NAME
if [ $? -eq 0 ]; then
  echo "Successfully built: $OUTPUT_DIR/$OUTPUT_NAME"
else
  echo "Failed to build $PACKAGE_NAME" >&2
fi

echo "All builds completed!"
