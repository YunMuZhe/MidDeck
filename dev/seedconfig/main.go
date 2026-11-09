// Command seedconfig adds connections for the dev playground (dev/docker-compose.yml)
// to the local MidDeck config store. Existing connections with the same name are updated.
//
//	go run ./dev/seedconfig
package main

import (
	"fmt"
	"log"

	"github.com/YunMuZhe/MidDeck/internal/config"
)

func main() {
	dir, err := config.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}
	store, err := config.Open(dir)
	if err != nil {
		log.Fatal(err)
	}
	bastion := []config.SSHHop{{Host: "localhost", Port: 2222, User: "ops", AuthType: "password", Secret: "middeck"}}

	want := []config.Instance{
		{
			Name: "Local Kafka", Kind: config.KindKafka, Project: "playground", Env: "dev",
			Addrs: []string{"localhost:9092"}, Tags: []string{"docker"}, Description: "Kafka 3.9 from dev/docker-compose.yml",
		},
		{
			Name: "Orders Kafka", Kind: config.KindKafka, Project: "order-service", Env: "prod",
			Addrs: []string{"kafka:19092"}, Aliases: []string{"订单 Kafka", "order-kafka"}, Tags: []string{"core"},
			Description: "Reached through the SSH bastion; kafka:19092 only resolves inside the Docker network.",
			Network:     config.Network{SSH: bastion},
		},
		{
			Name: "Session Cache", Kind: config.KindRedis, Project: "order-service", Env: "test",
			Addrs: []string{"localhost:6379"}, RedisMode: "standalone", Tags: []string{"cache"},
		},
		{
			Name: "Redis via Bastion", Kind: config.KindRedis, Project: "playground", Env: "staging",
			Addrs: []string{"redis:6379"}, RedisMode: "standalone", ReadOnly: true,
			Description: "Read-only connection through the SSH bastion.", Network: config.Network{SSH: bastion},
		},
		{
			Name: "Payment MQ", Kind: config.KindRabbitMQ, Project: "payment", Env: "staging",
			Addrs: []string{"localhost:5672"}, MgmtURL: "http://localhost:15672", VHost: "/",
			Auth: config.Auth{Username: "guest", Password: "guest"}, Aliases: []string{"支付 MQ"},
		},
		{
			Name: "Legacy Kafka", Kind: config.KindKafka, Project: "playground", Env: "dev",
			Addrs:       []string{"kafka-legacy.corp.internal:9092"},
			Description: "Intentionally unreachable, to show the diagnostics view.",
		},
	}

	byName := map[string]string{}
	for _, in := range store.List() {
		byName[in.Name] = in.ID
	}
	for _, in := range want {
		in.ID = byName[in.Name]
		saved, err := store.Save(in)
		if err != nil {
			log.Fatalf("%s: %v", in.Name, err)
		}
		fmt.Printf("saved %-20s %s\n", saved.Name, saved.ID)
	}
}
