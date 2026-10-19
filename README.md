# Guardaian

A small Go wrapper that asks a local Ollama decision model to score the risk of a shell command before executing it.

## Features

- Sends the command to Ollama using the decision API at `/v1/systemone`.
- Evaluates a risk score between 0.0 and 1.0.
- Allows execution when the score is below a configured threshold.
- Requires a typed `yes` confirmation when the score is at or above the threshold.
- Can be used as a one-off CLI or as a Bash pre-exec hook.

## Prerequisites

Make sure the local Ollama service is running:

```bash
ollama serve
```

Make sure the model you want to use is pulled locally. For the current setup, this is typically:

```bash
ollama pull tev1:0.8b
```

## Build

```bash
go build -o guard .
```

## Configuration

The guard reads settings from environment variables or the command-line flags shown below.

### Environment variables

```bash
export GUARDAIAN_MODEL=tev1:0.8b
export GUARDAIAN_THRESHOLD=0.50
export GUARDAIAN_OLLAMA_URL=http://localhost:11434
```

### Flags

```bash
./guard --model tev1:0.8b --threshold 0.75 --url http://localhost:11434 -- ls -la
```

The important values are:

- `--model`: Ollama model name.
- `--threshold`: risk threshold between `0.0` and `1.0`.
- `--url`: base URL for the local Ollama endpoint.

## Shell hook usage

Install the hook in your current Bash session:

```bash
source ./guard-shell
```

This enables the guard and tags the prompt with `(guardaian)` while active.

To disable it later:

```bash
guardaian-off
```

To re-enable it:

```bash
guardaian-on
```

The hook evaluates the current command before execution, including redirection forms like:

```bash
ls -l >myls.txt
```

The hook uses the guard in check-only mode and then lets Bash continue with the original command once the policy check passes or a high-risk command is explicitly confirmed.

## One-off CLI usage

You can also evaluate a single command directly without the shell hook:

```bash
./guard --check-only -- ls -la
./guard --threshold 0.75 -- ls -la
./guard --threshold 0.30 -- rm -rf /tmp/demo
```

When used with `--check-only`, the guard only evaluates and does not execute the command.

## Notes

The guard sends a request to Ollama’s `/v1/systemone` endpoint with a `state` string and `questions` map. For these decision models, the result is read from the `answers.<name>.noul` probability. The shell hook uses the last command line in Bash so it can evaluate the real command string before execution.

The default threshold is `0.5`. Any command scoring at or above that value triggers a warning and requires the user to type `yes` before proceeding.
