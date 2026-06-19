# Setup — Linux

## Requirements

- Go 1.22+
- A running [Costguard](https://github.com/marcoantonios1/costguard) instance
- A model served via Costguard (Ollama, Anthropic, OpenAI, etc.)

## 1. Install Go

If Go is not already installed:

```bash
# Download the latest Go tarball from https://go.dev/dl/ then:
sudo tar -C /usr/local -xzf go1.22.*.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version
```

## 2. Start Costguard

Costguard is the AI gateway Forge routes all model calls through. You need it running before Forge will work.

**Option A — Docker (recommended):**

```bash
git clone https://github.com/marcoantonios1/costguard
cd costguard
docker compose up --build
```

**Option B — Go:**

```bash
git clone https://github.com/marcoantonios1/costguard
cd costguard
go run ./cmd/api -config ./config.json
```

Costguard listens on `http://localhost:8080` by default. See the [Costguard README](https://github.com/marcoantonios1/costguard) for configuration options (providers, budgets, routing).

## 3. Clone and configure Forge

```bash
git clone https://github.com/marcoantonios1/Forge
cd Forge
cp .env.example .env
```

Open `.env` and fill in your Costguard URL and model names.

## 4. Add environment variables to your shell

```bash
nano ~/.bashrc
```

Copy each `KEY=value` line from `.env` and paste them at the bottom of `~/.bashrc`. Then reload:

```bash
source ~/.bashrc
echo $COSTGUARD_URL   # should print your Costguard URL
```

> If you use zsh, edit `~/.zshrc` instead.

## 5. Build and install

```bash
go build -o bin/forge ./cmd/forge
sudo cp bin/forge /usr/local/bin/forge
```

`forge` is now available globally:

```bash
cd ~/some-other-project
forge
```

## Uninstall

```bash
sudo rm /usr/local/bin/forge
```

Remove the env vars you added from `~/.bashrc`, then remove the build output:

```bash
cd ~/Forge
make clean
```
