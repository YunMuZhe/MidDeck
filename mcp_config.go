package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/YunMuZhe/MidDeck/internal/config"
	"github.com/YunMuZhe/MidDeck/internal/mcp"
)

// Config tools manage MidDeck's own connection list and jump hosts. They never
// return secrets; they accept them, because on a server there is no other way
// to set a password.

const instanceFields = `Instance fields (JSON, camelCase):
  name (required), kind (required: kafka | redis | rabbitmq), project, env (dev | test | staging | prod | …),
  addrs (list of host:port: Kafka bootstrap brokers, Redis nodes, RabbitMQ AMQP host:port),
  tags, aliases, description, readOnly (bool: refuse writes in MidDeck),
  redisMode (standalone | cluster | sentinel), redisDb, masterSet (sentinel),
  mgmtUrl (RabbitMQ management API, e.g. http://host:15672), vhost,
  auth {mechanism (Kafka: none | plain | scram-sha-256 | scram-sha-512), username, password},
  tls {enabled, insecureSkipVerify, caFile, certFile, keyFile},
  network {ssh: [{bastionId} or {host, port, user, authType (password | key | agent), keyPath, secret}], proxy {type: socks5, addr, user, secret}, addrMap {"advertised:port": "reachable:port"}},
  registry {url, username, password, insecureSkipVerify} (Kafka Schema Registry).`

// instanceView is an instance without secrets, saying which ones are set.
type instanceView struct {
	config.Instance
	HasPassword         bool   `json:"hasPassword,omitempty"`
	HasRegistryPassword bool   `json:"hasRegistryPassword,omitempty"`
	HasProxySecret      bool   `json:"hasProxySecret,omitempty"`
	SSHSecrets          []bool `json:"sshSecretsSet,omitempty"`
	FromRepo            bool   `json:"fromRepo,omitempty"`
	Note                string `json:"note,omitempty"`
}

func (a *App) view(full config.Instance) instanceView {
	v := instanceView{Instance: a.strip(full), HasPassword: full.Auth.Password != "", HasRegistryPassword: full.Registry.Password != "",
		HasProxySecret: full.Network.Proxy.Secret != "", FromRepo: a.fromRepo(full.ID)}
	for _, h := range full.Network.SSH {
		v.SSHSecrets = append(v.SSHSecrets, h.Secret != "")
	}
	if v.FromRepo {
		v.Note = "comes from the synced Git repository: the next sync overwrites changes other than passwords, and restores it if deleted"
	}
	return v
}

func checkInstance(in config.Instance) error {
	switch in.Kind {
	case config.KindKafka, config.KindRedis, config.KindRabbitMQ:
	default:
		return fmt.Errorf("kind must be kafka, redis or rabbitmq, not %q", in.Kind)
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name is required")
	}
	if len(in.Addrs) == 0 && in.MgmtURL == "" {
		return errors.New("addrs is required")
	}
	for _, h := range in.Network.SSH {
		if h.BastionID == "" && h.Host == "" {
			return errors.New("each network.ssh hop needs bastionId or host")
		}
	}
	return nil
}

// merge applies a JSON merge patch (RFC 7386): objects merge, null removes, the rest replaces.
func merge(dst, patch map[string]any) {
	for k, v := range patch {
		if v == nil {
			delete(dst, k)
			continue
		}
		if pm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				merge(dm, pm)
				continue
			}
		}
		dst[k] = v
	}
}

func decodeInto(v any, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (a *App) resolveBastion(ref string) (config.Bastion, error) {
	ref = strings.TrimSpace(ref)
	for _, b := range a.store.ListBastions() {
		if b.ID == ref || strings.EqualFold(b.Name, ref) {
			return a.store.GetBastion(b.ID)
		}
	}
	return config.Bastion{}, fmt.Errorf("no jump host matches %q; call bastion_list", ref)
}

func configTools(a *App) []mcp.Tool {
	inst := map[string]any{"type": "string", "description": "instance id, name or alias (see list_instances)"}
	obj := func(desc string) map[string]any { return map[string]any{"type": "object", "description": desc} }
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	tools := []mcp.Tool{
		{
			Name: "instance_get", Title: "Show a connection's settings",
			Description: "Full settings of one saved connection, without secrets (hasPassword etc. say which are set). fromRepo marks connections maintained by the synced Git repository.",
			Properties:  map[string]any{"instance": inst},
			Required:    []string{"instance"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := a.resolve(mcp.Str(args, "instance"))
				if err != nil {
					return nil, err
				}
				full, err := a.store.Get(in.ID)
				if err != nil {
					return nil, err
				}
				return a.view(full), nil
			},
		},
		{
			Name: "instance_create", Title: "Add a connection", Writes: true,
			Description: "Save a new Kafka, Redis or RabbitMQ connection in MidDeck. Use server_info afterwards to test it.\n" + instanceFields,
			Properties:  map[string]any{"config": obj("the instance fields; id is generated")},
			Required:    []string{"config"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				var in config.Instance
				if err := decodeInto(args["config"], &in); err != nil {
					return nil, fmt.Errorf("config: %w", err)
				}
				in.ID = ""
				if err := checkInstance(in); err != nil {
					return nil, err
				}
				saved, err := a.SaveInstance(in)
				a.audit(saved, "config.create", map[string]any{"name": in.Name, "via": "mcp"}, err)
				if err != nil {
					return nil, err
				}
				full, _ := a.store.Get(saved.ID)
				return a.view(full), nil
			},
		},
		{
			Name: "instance_update", Title: "Change a connection", Writes: true,
			Description: "Change a saved connection with a JSON merge patch: listed fields replace the current ones, nested objects merge, null removes a field. " +
				"Example: {\"addrs\": [\"10.0.0.5:9092\"], \"auth\": {\"password\": \"…\"}}. Lists (addrs, tags, network.ssh) are replaced as a whole.\n" + instanceFields,
			Properties: map[string]any{"instance": inst, "changes": obj("the fields to change")},
			Required:   []string{"instance", "changes"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				ref, err := a.resolve(mcp.Str(args, "instance"))
				if err != nil {
					return nil, err
				}
				patch, ok := args["changes"].(map[string]any)
				if !ok {
					return nil, errors.New("changes must be an object")
				}
				cur, err := a.store.Get(ref.ID) // with secrets, so an unrelated change keeps them
				if err != nil {
					return nil, err
				}
				var doc map[string]any
				if err := decodeInto(cur, &doc); err != nil {
					return nil, err
				}
				delete(patch, "id")
				merge(doc, patch)
				var next config.Instance
				if err := decodeInto(doc, &next); err != nil {
					return nil, fmt.Errorf("changes: %w", err)
				}
				next.ID = cur.ID
				if err := checkInstance(next); err != nil {
					return nil, err
				}
				fields := make([]string, 0, len(patch))
				for k := range patch {
					fields = append(fields, k)
				}
				_, err = a.SaveInstance(next)
				a.audit(next, "config.update", map[string]any{"fields": fields, "via": "mcp"}, err)
				if err != nil {
					return nil, err
				}
				full, _ := a.store.Get(cur.ID)
				return a.view(full), nil
			},
		},
		{
			Name: "instance_delete", Title: "Delete a connection", Writes: true, Destructive: true,
			Description: "Delete a saved connection and its stored passwords from MidDeck. The middleware itself is not touched.",
			Properties:  map[string]any{"instance": inst},
			Required:    []string{"instance"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				in, err := a.resolve(mcp.Str(args, "instance"))
				if err != nil {
					return nil, err
				}
				repo := a.fromRepo(in.ID)
				err = a.DeleteInstance(in.ID)
				a.audit(in, "config.delete", map[string]any{"name": in.Name, "via": "mcp"}, err)
				if err != nil {
					return nil, err
				}
				out := map[string]any{"deleted": in.Name, "id": in.ID}
				if repo {
					out["note"] = "this connection comes from the synced Git repository and returns on the next sync; remove it from the repository to delete it for good"
				}
				return out, nil
			},
		},
		{
			Name: "bastion_list", Title: "List jump hosts",
			Description: "Shared SSH jump hosts, without secrets, with the connections that use each.",
			Handler: func(_ context.Context, _ map[string]any) (any, error) {
				return a.store.ListBastions(), nil
			},
		},
		{
			Name: "bastion_save", Title: "Add or change a jump host", Writes: true,
			Description: "Create a shared SSH jump host, or change one when id (or name of an existing one) is given. " +
				"Fields: id, name, description, host, port (default 22), user, authType (password | key | agent), keyPath, secret (password or key passphrase; omit to keep the current one). " +
				"Connections use it with network.ssh: [{\"bastionId\": \"…\"}].",
			Properties: map[string]any{"bastion": obj("the jump host fields")},
			Required:   []string{"bastion"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				var b config.Bastion
				if err := decodeInto(args["bastion"], &b); err != nil {
					return nil, fmt.Errorf("bastion: %w", err)
				}
				if b.ID == "" && b.Name != "" {
					if old, err := a.resolveBastion(b.Name); err == nil {
						b.ID = old.ID
					}
				}
				if b.Name == "" || b.Host == "" {
					return nil, errors.New("name and host are required")
				}
				if b.Port == 0 {
					b.Port = 22
				}
				saved, err := a.SaveBastion(b)
				if err != nil {
					return nil, err
				}
				saved.Secret = ""
				return saved, nil
			},
		},
		{
			Name: "bastion_delete", Title: "Delete a jump host", Writes: true, Destructive: true,
			Description: "Delete a shared jump host. Fails while connections still use it.",
			Properties:  map[string]any{"bastion": str("jump host id or name")},
			Required:    []string{"bastion"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				b, err := a.resolveBastion(mcp.Str(args, "bastion"))
				if err != nil {
					return nil, err
				}
				if err := a.DeleteBastion(b.ID); err != nil {
					return nil, err
				}
				return map[string]any{"deleted": b.Name, "id": b.ID}, nil
			},
		},
	}
	if a.sync != nil {
		tools = append(tools, mcp.Tool{
			Name: "config_sync", Title: "Sync connections from Git",
			Description: "Pull the Git repository this server was started with (--repo) and apply its middeck/catalog.json now, instead of waiting for the next scheduled sync. Returns what changed.",
			Handler: func(ctx context.Context, _ map[string]any) (any, error) {
				return a.syncRepo(ctx)
			},
		})
	}
	return tools
}
