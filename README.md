# WhatsApp Message Capture Demo

A Go-based demonstration project that shows how to capture WhatsApp private messages using the [whatsmeow](https://github.com/tulir/whatsmeow) library.

## Features

- Connect to WhatsApp using QR code authentication
- Capture and log private messages
- Handle WhatsApp events and message types

## Prerequisites

- Go 1.16 or higher
- A WhatsApp account
- SQLite (for session storage)

## Installation

```bash
go mod download
```

## Usage

```bash
# generate QR code to Link Device with WhatsApp
go run main.go qr

# capture message
go run main.go message
```
