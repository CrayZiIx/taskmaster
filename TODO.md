# Taskmaster TODO

This checklist tracks the mandatory daemon/backend work. The TUI is being
implemented by my teammate and is therefore kept as an integration task here.

## 1. Configuration

- [x] Load a YAML configuration file.
- [x] Support one or more named programs under `taskmaster`.
- [x] Parse the command and all command-line arguments.
- [x] Validate that every program has a usable command.
- [x] Validate invalid values and combinations with clear error messages.
- [x] Apply the configured working directory (`workdir`).
- [x] Apply the configured file creation mask (`umask`).
- [x] Apply configured environment variables (`env`).
- [x] Configure stdout and stderr destinations (`output`).
- [x] Parse the process count (`process-nb`).
- [x] Parse the journey settings:
  - [x] `autostart`
  - [x] `health-time`
  - [x] restart case: `always`, `never`, or `unexpected`
  - [x] maximum restart count (`restart-nb`)
  - [x] accepted exit codes (`exit.code`)
  - [x] accepted exit signals (`exit.signal`)
  - [x] exit timeout (`exit.timeout-ms`)
  - [x] graceful stop signal (`stop.signal`)
- [x] Define and document defaults for optional configuration values.
- [x] Reload the configuration safely without unnecessarily stopping running
  processes.
- [x] Add configuration tests for valid files, invalid YAML, unknown fields,
  missing fields, invalid values, and reloads.

## 2. Process management

- [x] Create a process from a program configuration.
- [x] Start a process and wait for its exit.
- [x] Track the process state and exit code.
- [x] Capture or redirect stdout and stderr.
- [x] Run every configured process instance independently.
- [x] Support `process-nb > 1` with distinct process instances.
- [x] Prevent invalid lifecycle operations, such as starting an already
  running process or stopping a process that never started.
- [x] Stop processes gracefully using the configured signal.
- [x] Escalate after the configured timeout when a process does not exit.
- [x] Correctly handle exit codes, signals, failed starts, and crashes.
- [x] Ensure child processes are reaped and no zombies are left behind.
- [x] Ensure output files and other resources are always closed.
- [x] Add process lifecycle and concurrency tests.

## 3. Supervisor and monitoring

- [x] Implement the supervisor that owns all configured programs.
- [x] Start programs marked with `autostart: true`.
- [x] Monitor every running process in its own goroutine.
- [x] Detect normal exits, unexpected exits, signal exits, and start errors.
- [x] Apply the restart policy:
  - [x] `never`: do not restart.
  - [x] `always`: restart after every exit when allowed.
  - [x] `unexpected`: restart only after an unexpected exit.
- [x] Enforce the maximum number of restarts.
- [x] Wait for `health-time` before considering a process healthy.
- [x] Reset or retain restart counters according to the chosen policy.
- [x] Avoid restart loops and add a small backoff where necessary.
- [x] Expose a safe snapshot of program and process statuses to the UI/client.
- [x] Handle supervisor shutdown and stop all children cleanly.
- [x] Handle `SIGTERM`, `SIGINT`, and relevant child-process signals.
- [x] Add supervisor tests for autostart, crashes, restart policies, limits,
  timeouts, and shutdown.

## 4. Backend interface for the TUI

- [x] Define the commands/actions the TUI needs:
  - [x] list programs and instances
  - [x] show status and exit information
  - [x] start a program
  - [x] stop a program
  - [x] restart a program
  - [x] reload configuration
  - [x] shut down the supervisor
- [x] Define stable request/response and error types.
- [ ] Agree on this interface with my teammate before the TUI is implemented.
- [ ] Integrate the TUI with the supervisor without duplicating process logic.
- [x] Test the backend independently from the TUI.

## 5. Reliability and delivery

- [x] Run `go test ./...` successfully.
- [x] Run the race detector and fix detected data races.
- [x] Test commands that exit immediately, fail to start, run forever, and
  ignore the first stop signal.
- [x] Test multiple programs and multiple instances at the same time.
- [x] Test missing files, inaccessible output paths, invalid working
  directories, and invalid configuration values.
- [x] Review goroutine, file-descriptor, and process cleanup.
- [ ] Add useful logs for starts, stops, exits, restarts, and errors.
- [ ] Write build and usage instructions.
- [x] Add a complete example configuration.
- [ ] Perform a clean-clone build and final manual test.

## Bonus: Unix socket control interface

This is optional and should only be started after the mandatory supervisor is
stable.

- [ ] Choose a socket path and make it configurable.
- [ ] Create and remove the Unix domain socket safely during daemon startup and
  shutdown.
- [ ] Define a small protocol for requests and responses.
- [ ] Implement commands for status, start, stop, restart, reload, and quit.
- [ ] Return structured success and error responses.
- [ ] Handle malformed requests and disconnected clients.
- [ ] Prevent multiple daemons from using the same socket accidentally.
- [ ] Set appropriate socket permissions and document the security model.
- [ ] Add a small CLI client or make the TUI use the socket.
- [ ] Add client/server integration tests.

## Definition of done

- [ ] Every mandatory item above is implemented or explicitly explained in the
  project documentation.
- [ ] The daemon manages all configured process instances reliably.
- [ ] The TUI can control the backend through the agreed interface.
- [x] Tests pass, including race and cleanup tests.
- [ ] The Unix socket bonus is either complete or clearly marked as optional.
