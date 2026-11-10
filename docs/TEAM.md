# Team collaboration

Connect MidDeck to a Git repository to keep connection settings in sync across your team.

## Set up

1. Create an empty repository on your Git server and clone it on each computer.
2. In each clone set who you are: `git config user.name "…"` and `git config user.email "…"`, so commits have an author.
3. In MidDeck click **Team** (bottom left) → *Choose repository folder* and pick the clone.

The repository holds one file, `middeck/catalog.json`: connections and jump hosts, **without passwords**. MidDeck pulls when you open Team or click **Sync**, and commits and pushes when you publish.

## Syncing connections

*Team* compares your connections with the team's and marks each one *in sync*, *differs*, *team only* or *not shared*.

- **Publish** writes exactly the connections you select (plus the ones only teammates have) into `catalog.json`, then commits and pushes.
- **Import** adds or updates the selected connections on this computer and keeps passwords you already saved.

Passwords, SSH secrets and proxy secrets are never written to the repository; everyone fills in their own.
