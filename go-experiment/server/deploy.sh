#!/bin/bash
# Build, upload, and redeploy the warden server

set -e

SSH_SERVER=${SSH_SERVER:-$1}
SSH_USER=${SSH_USER:-${2:-$USER}}

cd $(dirname "$0")

# Build warden server for Linux
GOOS=linux go build -o warden-server server.go
echo "Built server..."
ls -lh warden-server

# scp the bits
echo "Uploading binary to $SSH_USER@$SSH_SERVER"
scp warden-server $SSH_USER@$SSH_SERVER:/opt/warden

# Restart the wardens server
ssh -t $SSH_USER@$SSH_SERVER "sudo service warden-server restart"

# Clean up the local binary
rm warden-server