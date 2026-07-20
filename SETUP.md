# Setup

floppygit needs git and the GitHub CLI on your PATH. It opens and merges pull requests through gh, so log in once if you haven't:

```sh
gh auth login
```

## Install a release

Every platform's archive is on the [releases page](https://github.com/aperswal/floppygit-mono/releases). The commands below download and install the current version.

### macOS on Apple silicon

```sh
VERSION=0.1.0
curl -L "https://github.com/aperswal/floppygit-mono/releases/download/v${VERSION}/floppygit_${VERSION}_darwin_arm64.tar.gz" -o floppygit.tar.gz
tar -xzf floppygit.tar.gz floppygit
sudo install -m 0755 floppygit /usr/local/bin/floppygit
```

### macOS on Intel

```sh
VERSION=0.1.0
curl -L "https://github.com/aperswal/floppygit-mono/releases/download/v${VERSION}/floppygit_${VERSION}_darwin_amd64.tar.gz" -o floppygit.tar.gz
tar -xzf floppygit.tar.gz floppygit
sudo install -m 0755 floppygit /usr/local/bin/floppygit
```

### Linux

Use `linux_arm64` instead of `linux_amd64` on ARM machines.

```sh
VERSION=0.1.0
curl -L "https://github.com/aperswal/floppygit-mono/releases/download/v${VERSION}/floppygit_${VERSION}_linux_amd64.tar.gz" -o floppygit.tar.gz
tar -xzf floppygit.tar.gz floppygit
sudo install -m 0755 floppygit /usr/local/bin/floppygit
```

### Windows

In PowerShell (use `windows_arm64` on ARM machines):

```powershell
$V = "0.1.0"
Invoke-WebRequest "https://github.com/aperswal/floppygit-mono/releases/download/v$V/floppygit_${V}_windows_amd64.zip" -OutFile floppygit.zip
Expand-Archive floppygit.zip -DestinationPath "$Env:LOCALAPPDATA\floppygit"
```

Then add `$Env:LOCALAPPDATA\floppygit` to your PATH.

## Build from source

Needs Go 1.26 or newer.

```sh
git clone https://github.com/aperswal/floppygit-mono.git
cd floppygit-mono
go build -o floppygit ./cmd/floppygit
sudo install -m 0755 floppygit /usr/local/bin/floppygit
```

On Windows, build with `go build -o floppygit.exe ./cmd/floppygit` and put the exe on your PATH.

## Check it works

```sh
floppygit status
```

That runs git status through floppygit. If it prints your repo status, you're set. [HOW-IT-WORKS.md](HOW-IT-WORKS.md) takes it from here.
