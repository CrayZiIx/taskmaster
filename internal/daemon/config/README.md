# Taskmaster configuration

Configuration files contain one or more named programs below `taskmaster`:

```yaml
taskmaster:
  worker:
    cmd: ["/usr/local/bin/worker", "--foreground"]
    workdir: "/var/lib/worker"
    umask: "022"
    process-nb: 1
    env:
      APP_ENV: production
    output:
      stdout: "/var/log/worker.log"
      stderr: "/var/log/worker.err"
    journey:
      autostart: true
      health-time: 1000
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

`cmd` is required and its first value must resolve to an executable. Arguments
are passed unchanged. `process-nb` must be at least one. Restart cases are
`always`, `never`, and `unexpected`; a non-zero restart count is invalid with
`never`.

The defaults are:

| Field | Default |
| --- | --- |
| `workdir` | inherit the daemon working directory |
| `umask` | `022` |
| `process-nb` | `1` |
| `autostart` | `false` |
| `health-time` | `0` milliseconds |
| `restart-case` | `never` |
| `restart-nb` | `0` |
| `exit.code` | `[0]` |
| `exit.signal` | empty |
| `exit.timeout-ms` | `5000` milliseconds |
| `stop.signal` | `SIGTERM` |
| `output.stdout`, `output.stderr` | inherit the daemon streams |
| `env` | inherit the daemon environment |

Configuration parsing rejects unknown fields, invalid YAML, invalid values,
invalid signal names, invalid environment names, and missing programs or
commands. Reloading parses and validates the candidate before replacing the
active snapshot; a failed reload leaves the previous snapshot unchanged.
