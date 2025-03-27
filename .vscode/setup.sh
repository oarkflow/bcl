#!/bin/bash
# Adjust the extension folder name as needed
EXTENSION_NAME="bcl"
SOURCE_DIR="$(pwd)/extensions/${EXTENSION_NAME}"
TARGET_DIR="${HOME}/.vscode/extensions/${EXTENSION_NAME}"

# Copy the extension folder to the VS Code extensions directory
rm -rf "$TARGET_DIR" && cp -R "$SOURCE_DIR" "$TARGET_DIR"

echo "Extension copied to ${TARGET_DIR}"
code .  # Launch VS Code in the current directory
