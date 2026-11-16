# Remote control

Ghost can run without its terminal UI and accept session requests over HTTP.
This lets local tools and companion devices read sessions, send prompts, stop a
turn, and answer permission requests.

## Start the server

Run `ghost serve` from the project directory. It listens on `127.0.0.1:49375`
by default and prints the URL and bearer token when it starts.

```sh
ghost serve
```

The token is saved in `remote.json` beside Ghost's global configuration with
file mode `0600`. Set `GHOST_REMOTE_TOKEN` to provide your own token. You can
also pass `--token`, though an environment variable keeps it out of shell
history. Change the port with `--port`.

Copy the token printed by the server into the client environment before using
the examples below:

```sh
export GHOST_REMOTE_TOKEN='paste the printed token here'
```

The server only listens on loopback unless you pass an explicit address with
`--bind`. Keep a bearer token set when binding to a network interface. The API
does not provide TLS, so use network access only on a trusted network.

Every request, including the event stream, needs this header:

```sh
Authorization: Bearer YOUR_TOKEN
```

## Sessions

List recent sessions or read one session and its messages:

```sh
curl -H "Authorization: Bearer $GHOST_REMOTE_TOKEN" \
  'http://127.0.0.1:49375/api/session?limit=40&order=desc&archived=false'

curl -H "Authorization: Bearer $GHOST_REMOTE_TOKEN" \
  http://127.0.0.1:49375/api/session/SESSION_ID

curl -H "Authorization: Bearer $GHOST_REMOTE_TOKEN" \
  'http://127.0.0.1:49375/api/session/SESSION_ID/message?limit=60&order=desc'
```

Session times are Unix milliseconds. The server reports the working directory
where `ghost serve` was started. A session created through the API uses that
same directory:

```sh
curl -X POST -H "Authorization: Bearer $GHOST_REMOTE_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"location":{"directory":"/path/to/project"}}' \
  http://127.0.0.1:49375/api/session
```

Send a prompt without waiting for the model to finish, or interrupt the active
turn:

```sh
curl -X POST -H "Authorization: Bearer $GHOST_REMOTE_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"text":"Review the latest changes"}' \
  http://127.0.0.1:49375/api/session/SESSION_ID/prompt

curl -X POST -H "Authorization: Bearer $GHOST_REMOTE_TOKEN" \
  http://127.0.0.1:49375/api/session/SESSION_ID/interrupt
```

Ghost returns `409` if a turn is already running in that session. Compaction is
available at `POST /api/session/SESSION_ID/compact`. Runtime model changes
return `501`; choose the model in Ghost before starting the server.

## Permissions and events

Read pending approvals and answer one:

```sh
curl -H "Authorization: Bearer $GHOST_REMOTE_TOKEN" \
  http://127.0.0.1:49375/api/session/SESSION_ID/permission

curl -X POST -H "Authorization: Bearer $GHOST_REMOTE_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"decision":"once"}' \
  http://127.0.0.1:49375/api/session/SESSION_ID/permission/REQUEST_ID/reply
```

Choose `once`, `always`, or `reject`. A request can only be answered once; a
second reply returns `404`. Ghost does not currently ask interactive form
questions. Form reads return an empty list and replies return `501`.

Follow live session, message, tool, permission, and execution events with
Server Sent Events:

```sh
curl -N -H "Authorization: Bearer $GHOST_REMOTE_TOKEN" \
  http://127.0.0.1:49375/api/event
```

Each event is JSON in a `data:` frame. The stream starts with
`server.connected`, sends a heartbeat every 15 seconds, and can be reconnected
by fetching the session list and message history again. Event types include
`session.created`, `session.updated`, `session.deleted`,
`session.inbox.enqueued`, `session.execution.started`,
`session.execution.succeeded`, `session.execution.failed`,
`session.execution.interrupted`, `session.text.ended`, `session.tool.called`,
`session.tool.success`, `session.tool.failed`, `permission.asked`, and
`permission.replied`.

`GET /api/info` reports the API version. All routes require the bearer token;
invalid or missing tokens receive `401`.
