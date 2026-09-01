# Taskmaster

Taskmaster launches and supervises configured programs from an interactive
terminal. It can start several independent instances, monitor their health,
restart them according to policy, reload configuration, and stop them
gracefully.

The current control interface is an in-process terminal UI. The optional Unix
socket interface is not implemented yet.

## Requirements

- Go 1.23.4 or a compatible newer Go release.
- A supported terminal on macOS or Linux.
- Executable commands referenced by the configuration file.

## Build and test

Run these commands from the repository root:

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o taskmaster ./cmd
```

The last command creates the `taskmaster` executable in the repository root.
To build for Linux from another supported host:

```sh
GOOS=linux GOARCH=amd64 go build -o taskmaster-linux-amd64 ./cmd
```

## Run Taskmaster

The default configuration path is:

```text
internal/daemon/config/default-config.yaml
```

Because this path is relative, run the program from the repository root when
using the default configuration:

```sh
go run ./cmd
# or, after building:
./taskmaster
```

A configuration path can be supplied as the first argument:

```sh
go run ./cmd ./path/to/taskmaster.yaml
./taskmaster ./path/to/taskmaster.yaml
```

Each Taskmaster run creates a dedicated daemon log:

```text
logs/<UTC-timestamp>-<pid>/daemon.log
```

The file uses one JSON record per line. Its first record contains run metadata
such as the loaded configuration path, PID, and start time. Reloads keep using
the same file and append a reload event. Program stdout and stderr follow each
program's `output` configuration and are kept separate from daemon logs.

If the log directory cannot be created, Taskmaster reports the startup error
on stderr and does not start the supervisor.

## Interactive commands

The prompt accepts one command per line:

| Command | Description |
| --- | --- |
| `list` | List every configured program instance. |
| `status` | Show the status of every instance. |
| `status <program>` | Show the status of one program. |
| `start <program>` | Start the program's configured instances. |
| `stop <program>` | Stop active instances using `stop.signal`. |
| `restart <program>` | Stop and start the program's instances. |
| `reload` | Reload the startup configuration file. |
| `reload <path>` | Validate and reload another configuration file. |
| `quit` / `exit` / `shutdown` | Stop managed processes and leave the UI. |

`Ctrl-C` and `SIGTERM` also initiate a graceful supervisor shutdown. A second
stop request for an already stopped program returns an error instead of
silently changing its state.

The status output uses these main states:

- `RUNNING`: the process is running.
- `STARTING` or `STOPPING`: a lifecycle transition is in progress.
- `STOPPED`: the process has not started, exited normally, or was stopped by
  the supervisor.
- `FATAL`: the process exited unexpectedly or failed to start.

## Example configurations

Additional ready-to-run examples are available in
[`.docs/examples/`](.docs/examples/):

- [`minimal.yaml`](.docs/examples/minimal.yaml): the smallest configuration.
- [`multi-instance.yaml`](.docs/examples/multi-instance.yaml): three
  independent instances with autostart.
- [`restart-on-crash.yaml`](.docs/examples/restart-on-crash.yaml): an
  intentional crash loop with a restart limit.
- [`output-and-environment.yaml`](.docs/examples/output-and-environment.yaml):
  environment overrides and separate stdout/stderr log files.
- [`expected-exit-signal.yaml`](.docs/examples/expected-exit-signal.yaml):
  demonstrates that `exit.signal` declares an accepted exit signal; it does
  not send one.

For example:

```sh
go run ./cmd .docs/examples/multi-instance.yaml
```

## Configuration

Programs are declared below the top-level `taskmaster` key. A minimal example
is:

```yaml
taskmaster:
  worker:
    cmd: ["/usr/local/bin/worker", "--foreground"]
    process-nb: 1
    journey:
      autostart: true
      restart-policy:
        restart-case: unexpected
        restart-nb: 3
      exit:
        code: [0]
        signal: ["SIGTERM"]
        timeout-ms: 5000
      stop:
        signal: SIGTERM
```

Important behavior:

- `cmd` is required; its first item must resolve to an executable. Remaining
  items are passed as arguments unchanged.
- `process-nb` controls how many independent instances are created.
- `autostart` starts the program when the supervisor launches.
- `restart-case` accepts `always`, `never`, or `unexpected`.
- `restart-nb` limits automatic restarts. Restart counters are tracked per
  instance and reset after the configured health period.
- `exit.code` and `exit.signal` describe exits considered valid. In
  particular, `exit.signal` does not kill a process; it declares which
  received termination signals are expected.
- `stop.signal` is the signal sent when `stop` or shutdown is requested.
- `exit.timeout-ms` is the graceful-stop timeout before escalation.
- Empty output paths inherit the daemon's stdout and stderr.
- Unknown YAML fields, invalid values, invalid signal names, invalid working
  directories, and unusable commands are rejected before a configuration is
  applied.

For the complete field list, defaults, and validation rules, see
[`internal/daemon/config/README.md`](internal/daemon/config/README.md). The
repository also includes a working example at
[`internal/daemon/config/default-config.yaml`](internal/daemon/config/default-config.yaml).

## Reloading safely

`reload` parses and validates the complete candidate configuration before it
replaces the active one. If validation fails, the current configuration and
running processes are preserved. Programs whose launch settings did not
change keep their running instances; added, removed, or changed programs are
reconciled by the supervisor.

## Clean-checkout verification

To reproduce the delivery checks from a clean checkout:

```sh
git clone <repository-url> taskmaster
cd taskmaster
go test ./...
go test -race ./...
go vet ./...
go build ./...
go run ./cmd
```

Then verify `list`, `status`, `start`, `stop`, `restart`, `reload`, and `quit`
against the example configuration. Replace `<repository-url>` with the
project's Git remote URL.
