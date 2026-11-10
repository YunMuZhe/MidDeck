# Changesets

A changeset is a YAML file that describes Kafka topics, or RabbitMQ exchanges, queues and bindings. MidDeck compares it with a live connection (**plan**) and makes the difference real (**apply**). The same file can be applied to dev, staging and prod in turn, and it runs from the app or from CI.

- **Additive.** Only what the file lists is touched. Topics or queues the file does not mention are left alone.
- **Explicit removal.** `state: absent` deletes a resource, and only when deletions are allowed (`--allow-delete`, or the checkbox in the app).
- **Reviewed before it runs.** Apply makes the plan again and refuses if it no longer matches the one you reviewed.
- **Guarded.** Read-only connections refuse to apply. Every change goes to the audit log. Production asks you to type the connection name.

## In the app

Open a Kafka or RabbitMQ connection and click **Changeset** in the header.

- **Open / Save** read and write a YAML file.
- **Load from a connection** exports the current topology of this connection or of another one of the same kind. To promote staging to prod, load staging while you are on the prod connection, then click Plan.
- **env** picks which `envs.<name>` overrides to use. It defaults to the connection's environment.
- **Plan** lists what will be created, updated or deleted, plus conflicts and warnings. **Apply** runs it.

## File format

```yaml
version: 1

kafka:
  defaults:                # fill in whatever a topic leaves out
    replication: 3
    configs:
      min.insync.replicas: "2"
  topics:
    - name: order-events
      partitions: 12
      configs:
        retention.ms: "604800000"
    - name: order-refund
      partitions: 6
    - name: legacy-events
      state: absent        # deleted only when deletions are allowed

rabbitmq:
  exchanges:
    - name: orders
      type: topic          # direct (default) | topic | fanout | headers
  queues:
    - name: order-refund
      type: quorum         # classic (default) | quorum | stream
      arguments:
        x-delivery-limit: 5
  bindings:
    - exchange: orders
      queue: order-refund  # or toExchange: <name>
      routingKey: order.refund.*

envs:                      # per-environment overrides, Kafka only
  dev:
    kafka:
      defaults:
        replication: 1     # a one-broker dev cluster
      topics:
        order-events: {partitions: 3}
```

For Kafka topics, later sources override earlier ones in this order:

1. `kafka.defaults`
2. the topic's own fields
3. `envs.<env>.kafka.defaults`
4. `envs.<env>.kafka.topics.<name>`

A file may have both a `kafka` and a `rabbitmq` section. Each connection uses the section for its own kind.

### What a plan can and cannot change

| | Create | Change in place | Left alone, with a warning or conflict |
|---|---|---|---|
| Kafka topic | ✓ | add partitions, set configs | fewer partitions, a different replication factor |
| Exchange | ✓ | — | a different type or flags (conflict) |
| Queue | ✓ | — | a different type or arguments (conflict; use a policy or recreate the queue) |
| Binding | ✓ | — | — |

The app skips conflicts and applies the rest. The CLI refuses to apply when there are conflicts unless you pass `--skip-conflicts`.

## CLI and CI

The app binary is also the CLI:

```bash
MidDeck changeset plan   -f topics.yaml -i "Orders Kafka (staging)"
MidDeck changeset apply  -f topics.yaml -i "Orders Kafka (prod)"
MidDeck changeset export -i "Orders Kafka (staging)" -o topics.yaml
```

`-i` accepts a saved connection's id, name or alias, and uses its jump hosts and keychain credentials. On macOS the binary is `/Applications/MidDeck.app/Contents/MacOS/MidDeck`.

CI runners have no saved connections, so describe the target with flags and pass secrets through the environment:

```bash
export MIDDECK_USERNAME=deployer MIDDECK_PASSWORD=$KAFKA_PASSWORD
MidDeck changeset plan  -f topics.yaml --kafka b1:9092,b2:9092 --sasl scram-sha-512 --tls --env prod --detailed-exitcode
MidDeck changeset apply -f topics.yaml --kafka b1:9092,b2:9092 --sasl scram-sha-512 --tls --env prod --auto-approve

MidDeck changeset apply -f queues.yaml --rabbitmq https://mq.internal:15672 --vhost orders --auto-approve
```

| Flag | Meaning |
|---|---|
| `--env` | which `envs.<env>` overrides to use |
| `--json` | print the plan as JSON (`plan` only) |
| `--detailed-exitcode` | `plan` exits with 2 when there are changes, 0 when there are none |
| `--auto-approve` | apply without the prompt (required when stdin is not a terminal) |
| `--allow-delete` | let `state: absent` delete resources |
| `--skip-conflicts` | apply the rest of the plan when some changes conflict |
| `--tls`, `--tls-insecure`, `--ca-file` | TLS for Kafka or the RabbitMQ management API |

Exit codes: 0 success, 1 error, 2 the plan has changes (only with `--detailed-exitcode`).

A GitHub Actions job that checks every pull request for drift:

```yaml
- name: Kafka topics drift
  run: |
    MidDeck changeset plan -f deploy/kafka.yaml \
      --kafka "$KAFKA_BOOTSTRAP" --sasl scram-sha-512 --tls --env staging --detailed-exitcode
  env:
    MIDDECK_USERNAME: ${{ secrets.KAFKA_USER }}
    MIDDECK_PASSWORD: ${{ secrets.KAFKA_PASSWORD }}
```

On Linux the binary links against WebKitGTK even in CLI mode, so the runner needs `libwebkit2gtk-4.1` installed.
