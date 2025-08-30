#!/bin/bash

# Build script for BCL language server extension

echo "Building BCL language server extension..."

# Create output directory if it doesn't exist
mkdir -p out

# Compile TypeScript files
echo "Compiling TypeScript files..."
npx tsc

# Check if compilation was successful
if [ $? -eq 0 ]; then
    echo "Compilation successful!"

    # Copy necessary files to out directory
    echo "Copying files..."
    cp package.json out/
    cp README.md out/
    cp CHANGELOG.md out/
    cp language-configuration.json out/
    cp -r syntaxes out/

    echo "Build completed successfully!"
else
    echo "Compilation failed!"
    exit 1
fi
