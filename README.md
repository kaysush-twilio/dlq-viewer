# DLQ Viewer

A TUI application to view identity-pipeline Dead Letter Queue (DLQ) messages from AWS SQS.

## Features

- View messages from Retryable and Failed DLQs
- Messages displayed in reverse chronological order
- Detail view with pretty-printed JSON bodies
- Auto-refresh every 30 seconds
- Support for multiple environments, regions, and cells

## Installation

### From Release

Download the latest binary from the [releases page](https://github.com/kaysush-twilio/dlq-viewer/releases).

### From Source

```bash
go install github.com/kaysush-twilio/dlq-viewer@latest
```

## Usage

```bash
# Basic usage (defaults to dev environment)
dlq-viewer

# Specify environment
dlq-viewer -e prod

# Specify all options
dlq-viewer --env prod --region us-west-2 --cell cell-1 --profile my-aws-profile
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--env` | `-e` | `dev` | Environment (dev, stage, prod) |
| `--region` | `-r` | `us-east-1` | AWS region |
| `--cell` | `-c` | `cell-1` | Cell identifier |
| `--profile` | `-p` | | AWS profile for credentials |

### Queue Names

Queue names are derived from the flags:
- Retryable: `identity-pipeline-retryable-{env}-{region}-{cell}`
- Failed: `identity-pipeline-failed-{env}-{region}-{cell}`

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑/k` | Navigate up |
| `↓/j` | Navigate down |
| `Enter` | View message details |
| `Esc` | Back to list |
| `Tab` | Switch between queues |
| `r` | Refresh messages |
| `q` | Quit |

## Requirements

- AWS credentials with SQS read permissions:
  - `sqs:GetQueueUrl`
  - `sqs:GetQueueAttributes`
  - `sqs:ReceiveMessage`
