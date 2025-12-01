# Connect3 (c3)

A minimal, terminal-based personal relationship manager (PRM). 
Track the people you meet and map how they connect to each other.

## Features

- **People:** Store names and notes.
- **Connections:** Link people together with a relationship strength (1-5) and description.
- **Graph View:** See who knows who in your network.
- **JSON Storage:** Data is saved locally in a human-readable format.

## Installation

You need [Go](https://go.dev/dl/) installed.

1. Clone or download the source code.
2. Build the executable with make 
   ```bash
   make build
   # or
   go build -o c3 ./cmd/connect3/main.go
   ```
3. Run the program
   ```bash
   ./c3
   ```
4. Add the binary to your $PATH or copy it to smth like ~/.local/bin
