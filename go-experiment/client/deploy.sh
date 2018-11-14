#!/bin/bash
# Build, upload, and redeploy the warden client

set -e

SSH_SERVER=${SSH_SERVER:-$1}
SSH_USER=${SSH_USER:-${2:-$USER}}

cd $(dirname "$0")

# Build warden client for Linux
GOOS=linux go build -o warden-client client.go
echo "Built client..."
ls -lh warden-client

# scp the bits
echo "Uploading binary to $SSH_USER@$SSH_SERVER"
scp warden-client $SSH_USER@$SSH_SERVER:/opt/warden

# Clean up the local binary
rm warden-client