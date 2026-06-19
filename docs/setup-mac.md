# Setup — macOS

## Requirements

- Go 1.22+
- A running [Costguard](https://github.com/marcoantonios1/costguard) instance
- A model served via Costguard (Ollama, Anthropic, OpenAI, etc.)

## 1. Start Costguard

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

## 2. Clone and configure Forge

```bash
git clone https://github.com/marcoantonios1/Forge
cd Forge
cp .env.example .env
```

Open `.env` and fill in your Costguard URL and model names.

## 3. Add environment variables to your shell

```bash
nano ~/.zshrc
```

Copy each `KEY=value` line from `.env` and paste them at the bottom of `~/.zshrc`. Then reload:

```bash
source ~/.zshrc
echo $COSTGUARD_URL   # should print your Costguard URL
```

> If you use bash instead of zsh, edit `~/.bash_profile` instead.

## 4. Build and install

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

Remove the env vars you added from `~/.zshrc`, then remove the build output:

```bash
cd ~/Documents/Forge
make clean
```
