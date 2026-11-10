package changeset

// Reference is the format description handed to AI tools that draft changesets.
// Keep it in step with spec.go and docs/CHANGESETS.md.
const Reference = `A MidDeck changeset is YAML describing the desired state of Kafka topics or RabbitMQ exchanges, queues and bindings.
MidDeck diffs it against a live instance (plan) and a person applies the difference.

version: 1

kafka:
  defaults:                 # optional; fills in what a topic leaves out
    partitions: 6
    replication: 3
    configs:
      min.insync.replicas: "2"
  topics:
    - name: order-events
      partitions: 12
      replication: 3
      configs:              # topic configs; values are strings
        retention.ms: "604800000"
        cleanup.policy: "delete"
    - name: legacy-events
      state: absent         # delete; only applied when the user allows deletions

rabbitmq:
  exchanges:
    - name: orders
      type: topic           # direct (default) | topic | fanout | headers
      durable: true         # default true
  queues:
    - name: order-refund
      type: quorum          # classic (default) | quorum | stream
      arguments:
        x-delivery-limit: 5
        x-dead-letter-exchange: orders.dlx
  bindings:
    - exchange: orders
      queue: order-refund   # or toExchange: <name>
      routingKey: order.refund.*

envs:                       # optional per-environment overrides, Kafka only
  dev:
    kafka:
      defaults: {replication: 1}
      topics:
        order-events: {partitions: 3}

Rules:
- Additive: list only what should change. Resources the file does not mention are left alone, so do not copy the whole topology unless asked.
- Unknown fields are rejected. Config values are quoted strings.
- A plan can create resources, add partitions and set topic configs. It cannot reduce partitions, change a replication factor, or change the type or arguments of an existing exchange or queue; those show up as warnings or conflicts.
- Use state: absent only when the user asked to delete something.
- A file may hold both a kafka and a rabbitmq section; each instance uses the section for its kind.
`
