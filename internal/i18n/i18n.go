// Package i18n translates user-facing backend messages (diagnostic steps,
// hints, compatibility notes). The UI language is set once from the frontend.
package i18n

import (
	"fmt"
	"sync/atomic"
)

var lang atomic.Value // "en" | "zh"

func init() { lang.Store("en") }

// SetLang switches the backend language. Unknown values fall back to English.
func SetLang(l string) {
	if l != "zh" {
		l = "en"
	}
	lang.Store(l)
}

// Lang returns the current language.
func Lang() string { return lang.Load().(string) }

// T formats the message for key in the current language.
func T(key string, args ...any) string {
	m, ok := messages[key]
	if !ok {
		return key
	}
	s := m[0]
	if Lang() == "zh" {
		s = m[1]
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// messages: key -> {English, Chinese}
var messages = map[string][2]string{
	// generic steps
	"step.config":          {"Configuration", "配置检查"},
	"step.tlsConfig":       {"TLS configuration", "TLS 配置"},
	"step.saslConfig":      {"SASL configuration", "SASL 配置"},
	"step.tcp":             {"TCP connect %s", "TCP 连通 %s"},
	"step.tls":             {"TLS handshake %s", "TLS 握手 %s"},
	"step.version":         {"Detect server version", "服务端版本识别"},
	"step.compat":          {"Version compatibility", "版本兼容性"},
	"step.network":         {"Network path", "网络路径"},
	"step.direct":          {"Direct (no proxy or SSH jump host)", "直连（未配置代理或 SSH 跳板）"},
	"step.proxy":           {"Proxy %s", "代理 %s"},
	"step.sshHop":          {"SSH hop %d (%s@%s)", "SSH 跳板 %d（%s@%s）"},
	"step.perm":            {"Permission: %s", "权限：%s"},
	"err.noAddr":           {"at least one address is required", "至少需要一个地址"},
	"err.noBootstrap":      {"at least one bootstrap address is required", "至少需要一个 bootstrap 地址"},
	"err.allUnreachable":   {"none of the bootstrap addresses are reachable", "所有 bootstrap 地址均不可达"},
	"err.connectFailed":    {"connection failed, see the diagnostic report", "连接失败，请查看诊断报告"},
	"err.readOnly":         {"instance %s is read-only, refused: %s", "实例 %s 为只读模式，已拒绝操作：%s"},
	"err.wrongKind":        {"instance %s is not this middleware type", "实例 %s 不是该类型的中间件"},
	"err.storeUnavailable": {"configuration store is unavailable", "配置存储不可用"},
	"err.unknownKind":      {"unknown kind %q", "未知类型 %q"},
	"err.needConfirm":      {"NEED_CONFIRM: this command may modify data or block the server", "NEED_CONFIRM: 该命令可能修改数据或阻塞服务，请确认后执行"},
	"compat.unavailable":   {"; unavailable: %s", "；不可用功能：%s"},
	"compat.tooOldHint":    {"Version is too old, read-only use is recommended.", "版本过旧，建议仅进行只读操作。"},

	// hints
	"hint.dns":        {"DNS lookup failed: the hostname may only resolve inside the intranet. Configure an SSH jump host or use an IP.", "DNS 解析失败：该域名可能只在内网可解析，可配置 SSH 跳板，或改用 IP。"},
	"hint.refused":    {"Connection refused: check the port and the listen address.", "端口拒绝连接：确认服务端口和监听地址。"},
	"hint.timeout":    {"Timed out: check network, VPN, security groups, or whether access must go through a bastion host.", "连接超时：检查网络、VPN、安全组，或是否需要通过堡垒机访问。"},
	"hint.tls":        {"TLS verification failed: configure the CA certificate, or skip verification in test environments.", "TLS 校验失败：配置 CA 证书，或在测试环境勾选“跳过证书校验”。"},
	"hint.auth":       {"Authentication failed: check username, password and mechanism.", "认证失败：检查用户名、密码和认证机制。"},
	"hint.sshAuth":    {"SSH authentication failed: check user, password or private key. If the bastion requires OTP, use key authentication.", "SSH 认证失败：检查用户名、密码或私钥。堡垒机若要求二次验证（OTP），请先使用私钥认证。"},
	"hint.sshHostKey": {"Host key differs from the recorded one: the host may have been reinstalled, or someone is intercepting. Remove the old entry from known_hosts once verified.", "主机指纹与记录不一致，可能是机器重装或存在中间人，确认后从 known_hosts 中删除旧记录。"},
	"hint.sshForward": {"The jump host forbids TCP forwarding (AllowTcpForwarding no). Ask ops to enable it.", "跳板机禁止了 TCP 转发（AllowTcpForwarding no），请联系运维开通。"},
	"hint.sshTimeout": {"Timed out: check address, port, VPN and firewall.", "连接超时：检查地址、端口、VPN 和防火墙。"},
	"hint.sshRefused": {"Port refused the connection: check the SSH port.", "端口拒绝连接：确认 SSH 服务端口是否正确。"},

	// kafka
	"kafka.metadata":     {"Metadata request", "元数据请求"},
	"kafka.saslMetadata": {"SASL/%s auth + metadata", "SASL/%s 认证 + 元数据请求"},
	"kafka.metadataOK":   {"cluster %s · brokers: %d · topics: %d", "集群 %s，%d 个 broker，%d 个 topic"},
	"kafka.broker":       {"Broker %d at %s", "Broker %d 地址 %s"},
	"kafka.mapped":       {"mapped to %s", "已映射到 %s"},
	"kafka.brokerHint":   {"The broker's advertised address (advertised.listeners) is unreachable from here. Map it to a reachable address under Address mapping, or configure an SSH jump host.", "broker 对外通告的地址（advertised.listeners）从本机不可达。在“地址映射”中把它映射到可达地址，或配置 SSH 跳板。"},
	"kafka.permTopics":   {"list topics", "列出 Topic"},
	"kafka.permGroups":   {"list consumer groups", "列出消费组"},
	"kafka.resetActive":  {"the group is %s; Kafka only allows resetting offsets when the group has no active members (Empty). Stop the consumers first", "消费组当前状态为 %s，Kafka 只允许在没有活跃消费者（Empty）时重置 offset，请先停止消费者"},

	// redis
	"redis.ping":            {"PING (with auth)", "PING（含认证）"},
	"redis.versionOK":       {"%s %s (%s) · %s", "%s %s（%s）· %s"},
	"redis.commands":        {"Command availability", "命令可用性"},
	"redis.disabled":        {"disabled or renamed: %s", "以下命令被禁用或改名：%s"},
	"redis.disabledHint":    {"Related features are unavailable (e.g. without SCAN keys cannot be browsed).", "相关功能将不可用（例如禁用 SCAN 时无法浏览 key）。"},
	"redis.compatFork":      {" (compatible implementation %s detected, some commands may behave differently)", "（检测到兼容实现 %s，部分命令行为可能不同）"},
	"redis.notExist":        {"key %q does not exist", "key %q 不存在"},
	"redis.unsupportedType": {"(viewer not available for type %s)", "（暂不支持查看该类型：%s）"},
	"redis.blocking":        {"blocking command %s is not supported in the console", "控制台不支持阻塞命令 %s"},
	"redis.quote":           {"unterminated quote", "引号未闭合"},

	// rabbitmq
	"rabbit.amqp":         {"AMQP port %s", "AMQP 端口连通 %s"},
	"rabbit.mgmt":         {"Management API %s", "Management API 连通 %s"},
	"rabbit.auth":         {"Auth + /api/overview", "认证 + /api/overview"},
	"rabbit.cluster":      {"cluster %v", "集群 %v"},
	"rabbit.needMgmt":     {"Management API URL is required, e.g. http://host:15672", "需要配置 Management API 地址，例如 http://host:15672"},
	"rabbit.needMgmtHint": {"Make sure the rabbitmq_management plugin is enabled.", "确认已启用 rabbitmq_management 插件。"},
	"rabbit.badURL":       {"invalid management URL: %q", "management URL 无效：%q"},
	"rabbit.401":          {"Wrong username or password, or the user lacks the management tag.", "用户名或密码错误，或该用户没有 management 标签。"},

	// compat
	"compat.noKind":                {"unknown middleware type", "未知中间件类型"},
	"compat.noVersion":             {"Could not detect the server version; some features may be unavailable", "无法识别服务端版本，部分功能可能不可用"},
	"compat.tested":                {"%s %s is covered by MidDeck integration tests", "%s %s 已通过 MidDeck 集成测试"},
	"compat.supported":             {"%s %s is within the supported range (>= %s)", "%s %s 在支持范围内（>= %s）"},
	"compat.partial":               {"%s %s is below the fully supported version %s; some features are unavailable", "%s %s 低于完整支持版本 %s，部分功能不可用"},
	"compat.unsupported":           {"%s %s is below the minimum supported version %s; read-only use is recommended", "%s %s 低于最低支持版本 %s，建议仅做只读查看"},
	"compat.requires":              {"%s (requires %s+)", "%s（需要 %s+）"},
	"feat.incrementalAlterConfigs": {"Incremental topic config changes", "增量修改 Topic 配置"},
	"feat.offsetDelete":            {"Delete consumer group offsets", "删除消费组 offset"},
	"feat.describeCluster":         {"DescribeCluster / KRaft info", "DescribeCluster / KRaft 信息"},
	"feat.kraft":                   {"KRaft mode (no ZooKeeper)", "KRaft 模式（无 ZooKeeper）"},
	"feat.stream":                  {"Streams", "Stream 数据类型"},
	"feat.acl":                     {"ACL users", "ACL 用户管理"},
	"feat.resp3":                   {"RESP3 protocol", "RESP3 协议"},
	"feat.functions":               {"Functions", "Functions"},
	"feat.hashFieldTTL":            {"Hash field TTL", "Hash 字段 TTL"},
	"feat.quorumQueue":             {"Quorum queues", "Quorum 队列"},
	"feat.streamQueue":             {"Stream queues", "Stream 队列"},
	"feat.mirroredQueue":           {"Classic mirrored queues (removed in 4.0)", "经典镜像队列（4.0 起移除）"},

	// dialogs
	"dlg.exportTitle":       {"Export connections", "导出连接配置"},
	"dlg.importTitle":       {"Import connections", "导入连接配置"},
	"dlg.bundle":            {"MidDeck bundle (*.json)", "MidDeck 配置包 (*.json)"},
	"dlg.exportMessages":    {"Export messages", "导出消息"},
	"dlg.exportDefinitions": {"Export definitions", "导出 definitions"},
	"share.notBundle":       {"not a MidDeck bundle", "不是 MidDeck 配置包"},
	"share.needPass":        {"this bundle is encrypted, a passphrase is required", "该配置包已加密，需要输入口令"},
	"share.wrongPass":       {"wrong passphrase or corrupted bundle", "口令错误或文件已损坏"},
	"share.imported":        {" (imported)", "（导入）"},
}
