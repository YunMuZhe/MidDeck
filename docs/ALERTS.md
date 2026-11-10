# Patrol alerts

MidDeck can watch your middleware in the background and show a system notification when something crosses a threshold. Open **Alerts** (bell, bottom left).

| Rule | Watches | Fires when |
|---|---|---|
| Consumer lag (Kafka) | Consumer groups, all or matching a pattern such as `order-*` | A group's total lag is at least the threshold, or grew by at least *growth* since the last check |
| Dead-letter queue (RabbitMQ) | Queues matching a pattern such as `*.dlq`; empty means queues that look like dead-letter queues (name contains `dlq` / `dead`, or used as a dead-letter target) | Messages ≥ threshold, or grew by ≥ *growth* |
| Big Redis key | Keys, all or matching a pattern | A key's `MEMORY USAGE` is at least the threshold |

- Checks only read. Kafka and RabbitMQ rules run every 15 s or more; Redis rules every 60 s or more and sample about 5,000 keys per check with `SCAN` + pipelined `MEMORY USAGE`, continuing where the previous check stopped, so large keyspaces are covered over several checks.
- A rule notifies when it starts firing, every 30 minutes while it keeps firing, and when it recovers. Three failed checks in a row also notify.
- Rules run only while MidDeck is open, on this computer. They are stored in `watch.json` in the data folder; history in `alerts.log`.
- Notifications use the system: Notification Center on macOS (allow notifications for *Script Editor* if nothing appears), `notify-send` on Linux, toast notifications on Windows. **Test notification** checks it.
