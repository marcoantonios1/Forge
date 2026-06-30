# Setup — Windows

> Forge is a terminal application. These instructions use **PowerShell** and assume you have [Git for Windows](https://git-scm.com/download/win) installed.

## Requirements

- Go 1.26+
- A running [Costguard](https://github.com/marcoantonios1/costguard) instance
- A model served via Costguard (Ollama, Anthropic, OpenAI, etc.)

## 1. Install Go

Download and run the installer from [go.dev/dl](https://go.dev/dl/). Verify:

```powershell
go version
```

## 2. Start Costguard

Costguard is the AI gateway Forge routes all model calls through. You need it running before Forge will work.

**Option A — Docker (recommended):**

```powershell
git clone https://github.com/marcoantonios1/costguard
cd costguard
docker compose up --build
```

**Option B — Go:**

```powershell
git clone https://github.com/marcoantonios1/costguard
cd costguard
go run ./cmd/api -config ./config.json
```

Costguard listens on `http://localhost:8080` by default. See the [Costguard README](https://github.com/marcoantonios1/costguard) for configuration options (providers, budgets, routing).

## 3. Clone and configure Forge

```powershell
git clone https://github.com/marcoantonios1/Forge
cd Forge
copy .env.example .env
```

Open `.env` in a text editor and fill in your Costguard URL and model names.

## 4. Set environment variables

Open **System Properties → Advanced → Environment Variables** and add each `KEY=VALUE` pair from your `.env` as a new User variable. Alternatively, set them permanently via PowerShell:

```powershell
[System.Environment]::SetEnvironmentVariable("COSTGUARD_URL", "http://localhost:8080", "User")
# repeat for each variable ...
```

Restart any open terminals for the changes to take effect.

## 5. Build and install

```powershell
go build -o bin\forge.exe .\cmd\forge
```

Copy the binary to a directory on your `PATH` (e.g. a custom `C:\bin` you've added to `PATH`):

```powershell
copy bin\forge.exe C:\bin\forge.exe
```

Verify:

```powershell
forge --help
```

## Uninstall

Delete the binary from wherever you placed it, and remove the environment variables you added via System Properties.
