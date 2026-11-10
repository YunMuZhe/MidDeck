# MidDeck on a Linux server (headless)

The headless build is MidDeck without a window: the **MCP server** and the **changeset CLI** in one static binary. It needs no desktop, GTK or WebKit, runs on any Linux distribution (amd64 / arm64), and is meant for a server inside your network, next to the middleware, so AI tools can use it over SSH.

Download `MidDeck-<version>-linux-<arch>-headless.tar.gz` from [Releases](https://github.com/YunMuZhe/MidDeck/releases):

```bash
tar -xzf MidDeck-*-linux-amd64-headless.tar.gz
sudo install MidDeck/MidDeck /usr/local/bin/middeck
middeck version
```

`git` must be installed if you load connections from a repository.

## Connections from a Git repository (GitLab, Gitea, GitHub…)

Point the server at the repository your team publishes connections to from the desktop app (*Team → Publish*). It holds `middeck/catalog.json`: connections and jump hosts, **without passwords**.

```bash
middeck mcp --repo https://oauth2:<token>@gitlab.example.com/ops/middeck-config.git
middeck mcp --repo git@gitlab.example.com:ops/middeck-config.git --branch main
middeck mcp --repo /srv/middeck-config          # an existing local clone
```

- At start MidDeck clones the repository (into its data folder) or pulls it, then imports the catalog. It pulls again every 5 minutes (`--sync-interval 10m`, `0` = only at start). The MCP tool `config_sync` syncs immediately.
- New and changed connections are applied; a connection removed from the repository is removed here. Connections you add on the server are never touched.
- Passwords are not in the repository. Set them on the server with `instance_update` (see below); later syncs keep them.
- Authentication is plain git: a deploy token in the URL (it is hidden in logs and errors), or an SSH deploy key for the user running MidDeck. Git never prompts.

## Managing connections through MCP

Besides the read tools (topics, lag, messages, keys, queues…) the server has tools for its own configuration. They change MidDeck's connection list, never the middleware:

| Tool | What it does |
|---|---|
| `list_instances`, `instance_get` | List connections; full settings of one, without secrets (`hasPassword` says whether one is set, `fromRepo` whether the repository maintains it) |
| `instance_create` | Add a connection |
| `instance_update` | Change fields with a JSON merge patch, e.g. `{"auth": {"password": "…"}}` |
| `instance_delete` | Remove a connection and its stored passwords |
| `bastion_list`, `bastion_save`, `bastion_delete` | Shared SSH jump hosts |
| `config_sync` | Pull the repository now (only with `--repo`) |

Changes are written to the audit log (`audit.log` in the data folder). Editing a connection that comes from the repository works until the next sync, which restores the repository version (passwords excepted); change the repository for anything lasting.

## Where data lives

| | |
|---|---|
| Data folder | `~/.config/MidDeck`, or `--data-dir DIR` / `$MIDDECK_DATA_DIR` |
| Connections | `instances.json` (mode 0600). The headless build keeps **passwords in this file**, since servers have no keychain; protect the account and the folder |
| Repository clone | `repo/` in the data folder |

Environment variables can replace the flags: `MIDDECK_REPO`, `MIDDECK_REPO_BRANCH`, `MIDDECK_SYNC_INTERVAL`, `MIDDECK_DATA_DIR`.

## Using it from Claude Code, Cursor and others

MCP runs over stdio, so the client starts the server, here through SSH:

```bash
claude mcp add middeck -- ssh -T ops@mcp-host.internal middeck mcp --repo git@gitlab.example.com:ops/middeck-config.git
```

For Cursor or any client with a JSON config:

```json
{
  "mcpServers": {
    "middeck": {
      "command": "ssh",
      "args": ["-T", "ops@mcp-host.internal", "middeck", "mcp", "--repo", "git@gitlab.example.com:ops/middeck-config.git"]
    }
  }
}
```

Use key-based SSH login (no password prompt). Each session starts its own server process; they share the data folder.

## Changesets from the command line

```bash
middeck changeset export -i "Orders Kafka" > orders.yaml
middeck changeset plan  -f orders.yaml -i "Orders Kafka"
middeck changeset apply -f orders.yaml -i "Orders Kafka" --auto-approve
```

See [CHANGESETS.md](CHANGESETS.md).
