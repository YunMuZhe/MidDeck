# MidDeck as an MCP server

The MidDeck binary doubles as a [Model Context Protocol](https://modelcontextprotocol.io) server over stdio:

```bash
MidDeck mcp
```

It serves the connections saved in the app, including ones behind SSH jump hosts, using the same keychain credentials. No tool changes a middleware. Creating topics, resetting offsets and other changes stay in the app, which has confirmation dialogs, read-only mode and the audit log. Tools write only MidDeck's own data: a changeset *file* (`changeset_propose`), which a person then opens, plans and applies in MidDeck, and the saved connections and jump hosts (`instance_*`, `bastion_*`).

To run it on a Linux server without a desktop, with connections loaded from a Git repository, see [SERVER.md](SERVER.md).

## Setup

The app shows ready-to-copy snippets with the right path under **Settings → Use in Claude Code / Cursor (MCP)**. The executable path depends on the platform:

| Platform | Path |
|---|---|
| macOS | `/Applications/MidDeck.app/Contents/MacOS/MidDeck` |
| Windows | `C:\Program Files\MidDeck\MidDeck.exe` (or wherever the portable exe lives) |
| Linux | wherever you extracted `MidDeck` |

**Claude Code**

```bash
claude mcp add middeck -- /Applications/MidDeck.app/Contents/MacOS/MidDeck mcp
```

**Cursor**: `~/.cursor/mcp.json`, or `.cursor/mcp.json` in a project:

```json
{
  "mcpServers": {
    "middeck": {
      "command": "/Applications/MidDeck.app/Contents/MacOS/MidDeck",
      "args": ["mcp"]
    }
  }
}
```

## Tools

| Tool | What it does |
|---|---|
| `list_instances` | Saved connections, filterable by words, kind and environment |
| `server_info` | Connect and report version, mode and compatibility; diagnostics on failure |
| `kafka_list_topics`, `kafka_describe_topic` | Topics, partitions, offsets, configs |
| `kafka_fetch_messages` | Read messages by newest / oldest / offset / time, with key and value filters; no offsets are committed |
| `kafka_list_groups`, `kafka_describe_group` | Consumer groups and per-partition lag |
| `redis_info`, `redis_slowlog` | Server overview and slow log |
| `redis_scan`, `redis_get` | Find keys with SCAN and read any type |
| `redis_command` | One read-only, non-blocking command (writes, `KEYS`, `FLUSHALL`… are refused) |
| `rabbit_list_queues`, `rabbit_list_exchanges`, `rabbit_queue_bindings` | Topology and queue depth |
| `rabbit_peek` | First messages of a queue. They are requeued, so they stay but are marked redelivered |
| `changeset_format` | The changeset YAML format, for writing one |
| `changeset_export` | The current topology of a Kafka or RabbitMQ connection as changeset YAML |
| `changeset_plan` | Dry run: what applying a YAML to a connection would create, update or delete. Changes nothing |
| `changeset_propose` | Save a YAML to MidDeck's local changeset library. Not applied |
| `instance_get` | Full settings of a connection, without secrets |
| `instance_create`, `instance_update`, `instance_delete` | Manage saved connections (update takes a JSON merge patch) |
| `bastion_list`, `bastion_save`, `bastion_delete` | Manage shared jump hosts |
| `config_sync` | Pull the Git repository given with `--repo` and apply its catalog (server mode) |

Clients that honour MCP annotations ask before the write and delete tools run.

### Changesets from an AI tool

Ask for a change in words; the tool reads the format, exports the current topology, writes YAML, checks it with `changeset_plan` and saves it with `changeset_propose`. Then in MidDeck open the connection → **Changeset → Library**, open the file, click **Plan**, review and **Apply**.

Tools take an `instance` argument: an id, the exact name, an alias, or a unique part of the name.

## Example prompts

- "Which consumer groups on the orders Kafka are lagging, and on which partitions?"
- "Show the last 5 messages in order-events whose value contains 1042."
- "What is stored under session:42 in the session cache?"
- "Which RabbitMQ queues on Payment MQ have messages but no consumers?"
- "Draft a changeset that adds topic order-refund (6 partitions) to the orders Kafka with the same configs as order-events, and plan it against staging."
