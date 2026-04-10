---
title: Installation
---

# Installation

## Using `go install`

```sh
go install github.com/dgageot/gogo@latest
```

Make sure `$GOPATH/bin` (or `$HOME/go/bin`) is in your `PATH`.

## From Source

```sh
git clone https://github.com/dgageot/gogo.git
cd gogo
go build -o gogo .
```

## Verify

```sh
gogo --help
```
