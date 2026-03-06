# Hello World Skill

A minimal skill example for OpenBotStack.

## Structure

```
hello-world/
├── main.go         # Skill source code
├── manifest.yaml   # Skill configuration
└── README.md       # This file
```

## Build

```bash
# Requires TinyGo
tinygo build -o main.wasm -target wasi main.go
```

## Deploy

```bash
# Zip the skill
zip -r hello-world.zip manifest.yaml main.wasm

# Upload (future API)
curl -X POST http://localhost:8080/admin/skills -F "skill=@hello-world.zip"
```

## Test locally

```bash
# Future CLI
openbotstack skill test ./
```
