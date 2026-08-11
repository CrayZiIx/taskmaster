# TUI backend contract

The TUI talks to `backend.Backend.Execute` with a `backend.Request` and reads
the returned `backend.Response`.

Supported actions are `list`, `status`, `start`, `stop`, `restart`, `reload`,
and `shutdown`.

`start`, `stop`, and `restart` require `Request.Program`. `reload` uses the
configuration path passed to `backend.New`; `Request.ConfigPath` may override
it for that request. Every successful action returns a copied status snapshot.

The backend owns command validation and delegates lifecycle behavior to the
supervisor. The TUI should not create processes, send signals, or implement
restart policy itself.
