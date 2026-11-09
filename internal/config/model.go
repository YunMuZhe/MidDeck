package config

// Kind is the middleware type of an instance.
type Kind string

const (
	KindKafka    Kind = "kafka"
	KindRedis    Kind = "redis"
	KindRabbitMQ Kind = "rabbitmq"
)

// SSHHop is one jump host in an SSH tunnel chain.
//
// When BastionID is set the hop refers to a shared jump host (see Bastion) and the
// remaining fields are filled in from it at connect time.
type SSHHop struct {
	BastionID string `json:"bastionId,omitempty"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	AuthType  string `json:"authType"` // password | key | agent
	KeyPath   string `json:"keyPath,omitempty"`
	// Password / key passphrase. Kept in the OS keychain, never written to the config file.
	Secret string `json:"secret,omitempty"`
}

// Bastion is a jump host maintained once and shared by many instances.
type Bastion struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SSHHop
}

// BastionView is a bastion as listed in the UI: no secret, plus who uses it.
type BastionView struct {
	Bastion
	HasSecret bool     `json:"hasSecret"`
	UsedBy    []string `json:"usedBy"`
}

// Proxy is an optional SOCKS5 proxy used instead of (or before) SSH hops.
type Proxy struct {
	Type string `json:"type"` // "" | socks5
	Addr string `json:"addr"`
	User string `json:"user,omitempty"`
	// Kept in the OS keychain.
	Secret string `json:"secret,omitempty"`
}

// Network describes how MidDeck reaches the middleware.
type Network struct {
	Proxy Proxy    `json:"proxy"`
	SSH   []SSHHop `json:"ssh"`
	// AddrMap rewrites addresses before dialing, e.g. Kafka advertised listeners
	// "10.0.0.5:9092" -> "kafka-1.internal:9092".
	AddrMap map[string]string `json:"addrMap,omitempty"`
}

// TLS settings.
type TLS struct {
	Enabled            bool   `json:"enabled"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	CAFile             string `json:"caFile,omitempty"`
	CertFile           string `json:"certFile,omitempty"`
	KeyFile            string `json:"keyFile,omitempty"`
}

// Auth settings. Mechanism meaning depends on Kind:
// kafka: none|plain|scram-sha-256|scram-sha-512; redis/rabbitmq: ignored.
type Auth struct {
	Mechanism string `json:"mechanism,omitempty"`
	Username  string `json:"username,omitempty"`
	// Kept in the OS keychain.
	Password string `json:"password,omitempty"`
}

// Instance is one middleware endpoint (cluster) MidDeck can connect to.
type Instance struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        Kind     `json:"kind"`
	Project     string   `json:"project"`
	Env         string   `json:"env"` // dev | test | staging | prod | custom
	Tags        []string `json:"tags"`
	Aliases     []string `json:"aliases"`
	Description string   `json:"description"`
	ReadOnly    bool     `json:"readOnly"`

	// Addresses: kafka bootstrap brokers, redis nodes, rabbitmq AMQP host:port.
	Addrs []string `json:"addrs"`
	// Redis
	RedisMode string `json:"redisMode,omitempty"` // standalone|cluster|sentinel
	RedisDB   int    `json:"redisDb,omitempty"`
	MasterSet string `json:"masterSet,omitempty"`
	// RabbitMQ management API, e.g. http://host:15672
	MgmtURL string `json:"mgmtUrl,omitempty"`
	VHost   string `json:"vhost,omitempty"`

	Auth    Auth    `json:"auth"`
	TLS     TLS     `json:"tls"`
	Network Network `json:"network"`
}

// IsProd reports whether the instance should get production safeguards.
func (i *Instance) IsProd() bool {
	return i.Env == "prod" || i.Env == "production"
}
