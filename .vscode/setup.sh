#!/bin/bash

# Enhanced setup script for BCL language extension
# This script installs and sets up the BCL language extension for VS Code

set -e  # Exit on any error

echo "Setting up BCL language extension..."

# Adjust the extension folder name as needed
EXTENSION_NAME="bcl"
SOURCE_DIR="$(pwd)/extensions/${EXTENSION_NAME}"
TARGET_DIR="${HOME}/.vscode/extensions/${EXTENSION_NAME}"

# Check if source directory exists
if [ ! -d "$SOURCE_DIR" ]; then
    echo "Error: Source directory $SOURCE_DIR does not exist"
    exit 1
fi

echo "Installing npm dependencies..."
cd "$SOURCE_DIR"
npm install

echo "Building the extension..."
./build.sh

echo "Creating VS Code extensions directory if it doesn't exist..."
mkdir -p "${HOME}/.vscode/extensions"

echo "Removing any existing extension installation..."
rm -rf "$TARGET_DIR"

echo "Copying extension files to VS Code extensions directory..."
cp -R "$SOURCE_DIR" "$TARGET_DIR"

echo "Extension successfully installed to ${TARGET_DIR}"

echo "Setup complete!"
echo ""
echo "To use the BCL language extension:"
echo "1. Restart VS Code"
echo "2. Open a .bcl file"
echo "3. The extension should automatically activate"
echo "4. You can now use features like:"
echo "   - Go to Definition (right-click or F12)"
echo "   - Find All References"
echo "   - Syntax error highlighting"
echo "   - Code completion"
echo "   - Hover information"
