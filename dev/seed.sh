#!/usr/bin/env bash
# Fills the dev playground (dev/docker-compose.yml) with realistic sample data.
set -euo pipefail
cd "$(dirname "$0")"
DC="docker compose -f docker-compose.yml"
K="$DC exec -T kafka /opt/kafka/bin"

echo "Waiting for services..."
until $K/kafka-broker-api-versions.sh --bootstrap-server kafka:19092 >/dev/null 2>&1; do sleep 2; done
until $DC exec -T rabbitmq rabbitmq-diagnostics -q check_running >/dev/null 2>&1; do sleep 2; done
until curl -sf -u guest:guest http://localhost:15672/api/overview >/dev/null; do sleep 2; done

echo "Kafka: topics"
topic() { $K/kafka-topics.sh --bootstrap-server kafka:19092 --create --if-not-exists --topic "$1" --partitions "$2" --replication-factor 1 ${3:+--config "$3"} >/dev/null; }
topic order-created 6 retention.ms=604800000
topic order-paid 6
topic order-refund-events 3
topic payment-callback 3 cleanup.policy=compact
topic inventory-changed 12
topic user-signup 2

echo "Kafka: messages"
gen() { # topic count
  for i in $(seq 1 "$2"); do
    uid=$((1000 + RANDOM % 9000)); amt=$((RANDOM % 50000)).$((RANDOM % 100))
    printf 'order-%d\t{"orderId":"o-%d","userId":%d,"amount":%s,"currency":"CNY","items":%d,"channel":"%s","createdAt":"2026-11-08T%02d:%02d:00Z"}\n' \
      "$i" "$i" "$uid" "$amt" $((1 + RANDOM % 5)) "$( [ $((RANDOM%2)) = 0 ] && echo app || echo web )" $((RANDOM%24)) $((RANDOM%60))
  done | $K/kafka-console-producer.sh --bootstrap-server kafka:19092 --topic "$1" \
           --property parse.key=true --property key.separator=$'\t' >/dev/null
}
gen order-created 800
gen order-paid 520
gen order-refund-events 37
gen payment-callback 300
gen inventory-changed 1500
gen user-signup 60

echo "Kafka: consumer groups"
reset() { $K/kafka-consumer-groups.sh --bootstrap-server kafka:19092 --group "$1" --topic "$2" --reset-offsets "${@:3}" --execute >/dev/null; }
reset order-service-consumer order-created --to-offset 90      # ~260 lag
reset refund-worker order-refund-events --to-earliest            # all 37 pending
reset payment-reconciler payment-callback --to-latest            # caught up
reset inventory-sync inventory-changed --shift-by -400 --to-latest 2>/dev/null || reset inventory-sync inventory-changed --to-offset 90
# A live consumer so one group shows as Stable with members.
$DC exec -T -d kafka sh -c '/opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server kafka:19092 --topic order-paid --group live-dashboard >/dev/null 2>&1'

echo "Redis: keys"
R="$DC exec -T redis redis-cli"
for i in $(seq 1 40); do $R SET "user:$i:profile" "{\"id\":$i,\"name\":\"user$i\",\"vip\":$([ $((i%5)) = 0 ] && echo true || echo false),\"city\":\"Shanghai\"}" >/dev/null; done
for i in $(seq 1 15); do $R SET "session:$(printf '%08x' $((RANDOM*RANDOM)))" "uid=$i" EX $((600 + RANDOM % 3000)) >/dev/null; done
$R HSET order:o-1001 status PAID amount 299.00 userId 1024 channel app createdAt 2026-11-08T09:12:00Z >/dev/null
$R HSET order:o-1002 status REFUNDING amount 58.50 userId 2048 channel web createdAt 2026-11-08T10:03:00Z >/dev/null
$R HSET config:feature-flags new-checkout on dark-mode off refund-v2 beta >/dev/null
$R RPUSH queue:email:pending '{"to":"a@example.com","tpl":"welcome"}' '{"to":"b@example.com","tpl":"reset"}' '{"to":"c@example.com","tpl":"receipt"}' >/dev/null
$R SADD tags:hot phone laptop headphones camera >/dev/null
for i in $(seq 1 20); do $R ZADD leaderboard:2026-11 $((RANDOM % 10000)) "player$i" >/dev/null; done
for i in $(seq 1 10); do $R XADD stream:audit '*' action login user "user$i" ip "10.0.0.$i" >/dev/null; done
$R SET counter:pv 1843920 >/dev/null
$R SET cache:product:42 '{"id":42,"title":"Noise Cancelling Headphones","price":1999,"stock":18}' EX 3600 >/dev/null

echo "RabbitMQ: exchanges, queues, messages"
api() { curl -sf -u guest:guest -H 'content-type: application/json' -X "$1" "http://localhost:15672/api/$2" ${3:+-d "$3"} >/dev/null; }
api PUT exchanges/%2F/order.events '{"type":"topic","durable":true}'
api PUT exchanges/%2F/dlx '{"type":"fanout","durable":true}'
api PUT exchanges/%2F/notify '{"type":"direct","durable":true}'
api PUT queues/%2F/order.created.billing '{"durable":true,"arguments":{"x-queue-type":"quorum","x-dead-letter-exchange":"dlx"}}'
api PUT queues/%2F/order.created.search '{"durable":true,"arguments":{"x-queue-type":"classic"}}'
api PUT queues/%2F/order.refund '{"durable":true,"arguments":{"x-queue-type":"quorum","x-dead-letter-exchange":"dlx","x-message-ttl":86400000}}'
api PUT queues/%2F/notify.sms '{"durable":true,"arguments":{"x-max-length":10000}}'
api PUT queues/%2F/dead-letters '{"durable":true}'
api POST bindings/%2F/e/order.events/q/order.created.billing '{"routing_key":"order.created"}'
api POST bindings/%2F/e/order.events/q/order.created.search '{"routing_key":"order.*"}'
api POST bindings/%2F/e/order.events/q/order.refund '{"routing_key":"order.refund.#"}'
api POST bindings/%2F/e/notify/q/notify.sms '{"routing_key":"sms"}'
api POST bindings/%2F/e/dlx/q/dead-letters '{"routing_key":""}'
pub() { api POST "exchanges/%2F/$1/publish" "{\"properties\":{\"delivery_mode\":2,\"content_type\":\"application/json\"},\"routing_key\":\"$2\",\"payload\":$(printf '%s' "$3" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))'),\"payload_encoding\":\"string\"}"; }
for i in $(seq 1 25); do pub order.events order.created "{\"orderId\":\"o-$((2000+i))\",\"amount\":$((RANDOM%900)).00}"; done
for i in $(seq 1 6); do pub order.events order.refund.requested "{\"orderId\":\"o-$((3000+i))\",\"reason\":\"damaged\"}"; done
for i in $(seq 1 12); do pub notify sms "{\"phone\":\"138****$((1000+i))\",\"tpl\":\"otp\"}"; done
for i in $(seq 1 3); do pub dlx "" "{\"orderId\":\"o-$((4000+i))\",\"error\":\"timeout calling payment gateway\"}"; done

echo "Done."
