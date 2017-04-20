#!/bin/bash
# Build, upload, and redeploy the warden EC2 agent

set -e

SSH_SERVER=${SSH_SERVER:-$1}
SSH_USER=${SSH_USER:-${2:-$USER}}

cd $(dirname "$0")

# Build warden EC2 agent for Linux
GOOS=linux go build -o warden-ec2-agent *.go
echo "Built EC2 agent..."
ls -lh warden-ec2-agent

# scp the bits
echo "Uploading binary to $SSH_USER@$SSH_SERVER"
scp warden-ec2-agent $SSH_USER@$SSH_SERVER:/opt/warden

# Restart the wardens EC2 agent
ssh -t $SSH_USER@$SSH_SERVER "sudo service warden-ec2-agent restart"

# Clean up the local binary
rm warden-ec2-agent