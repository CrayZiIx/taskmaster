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
- [x] Define and document defaults for optional configuration values.
- [ ] Reload the configuration safely without unnecessarily stopping running
  processes.
- [x] Add configuration tests for valid files, invalid YAML, unknown fields,
  missing fields, invalid values, and reloads.

## 2. Process management

- [x] Create a process from a program configuration.
- [x] Start a process and wait for its exit.
- [x] Track the process state and exit code.
- [x] Capture or redirect stdout and stderr.
- [ ] Run every configured process instance independently.
- [ ] Support `process-nb > 1` with distinct process instances.
- [ ] Prevent invalid lifecycle operations, such as starting an already
  running process or stopping a process that never started.
- [ ] Stop processes gracefully using the configured signal.
- [ ] Escalate after the configured timeout when a process does not exit.
- [ ] Correctly handle exit codes, signals, failed starts, and crashes.
- [ ] Ensure child processes are reaped and no zombies are left behind.
- [ ] Ensure output files and other resources are always closed.
- [ ] Add process lifecycle and concurrency tests.

## 3. Supervisor and monitoring

- [ ] Implement the supervisor that owns all configured programs.
- [ ] Start programs marked with `autostart: true`.
- [ ] Monitor every running process in its own goroutine.
- [ ] Detect normal exits, unexpected exits, signal exits, and start errors.
- [ ] Apply the restart policy:
  - [ ] `never`: do not restart.
  - [ ] `always`: restart after every exit when allowed.
  - [ ] `unexpected`: restart only after an unexpected exit.
- [ ] Enforce the maximum number of restarts.
- [ ] Wait for `health-time` before considering a process healthy.
- [ ] Reset or retain restart counters according to the chosen policy.
- [ ] Avoid restart loops and add a small backoff where necessary.
- [ ] Expose a safe snapshot of program and process statuses to the UI/client.
- [ ] Handle supervisor shutdown and stop all children cleanly.
- [ ] Handle `SIGTERM`, `SIGINT`, and relevant child-process signals.
- [ ] Add supervisor tests for autostart, crashes, restart policies, limits,
  timeouts, and shutdown.

## 4. Backend interface for the TUI

- [ ] Define the commands/actions the TUI needs:
  - [ ] list programs and instances
  - [ ] show status and exit information
  - [ ] start a program
  - [ ] stop a program
  - [ ] restart a program
  - [ ] reload configuration
  - [ ] shut down the supervisor
- [ ] Define stable request/response and error types.
- [ ] Agree on this interface with my teammate before the TUI is implemented.
- [ ] Integrate the TUI with the supervisor without duplicating process logic.
- [ ] Test the backend independently from the TUI.

## 5. Reliability and delivery

- [ ] Run `go test ./...` successfully.
- [ ] Run the race detector and fix detected data races.
- [ ] Test commands that exit immediately, fail to start, run forever, and
  ignore the first stop signal.
- [ ] Test multiple programs and multiple instances at the same time.
- [ ] Test missing files, inaccessible output paths, invalid working
  directories, and invalid configuration values.
- [ ] Review goroutine, file-descriptor, and process cleanup.
- [ ] Add useful logs for starts, stops, exits, restarts, and errors.
- [ ] Write build and usage instructions.
- [ ] Add a complete example configuration.
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
- [ ] Tests pass, including race and cleanup tests.
- [ ] The Unix socket bonus is either complete or clearly marked as optional.
