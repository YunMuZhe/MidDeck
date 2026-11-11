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
	"step.config":            {"Configuration", "配置检查"},
	"step.tlsConfig":         {"TLS configuration", "TLS 配置"},
	"step.saslConfig":        {"SASL configuration", "SASL 配置"},
	"step.tcp":               {"TCP connect %s", "TCP 连通 %s"},
	"step.tls":               {"TLS handshake %s", "TLS 握手 %s"},
	"step.version":           {"Detect server version", "服务端版本识别"},
	"step.compat":            {"Version compatibility", "版本兼容性"},
	"step.network":           {"Network path", "网络路径"},
	"step.direct":            {"Direct (no proxy or SSH jump host)", "直连（未配置代理或 SSH 跳板）"},
	"step.proxy":             {"Proxy %s", "代理 %s"},
	"step.sshHop":            {"SSH hop %d (%s@%s)", "SSH 跳板 %d（%s@%s）"},
	"step.perm":              {"Permission: %s", "权限：%s"},
	"err.noAddr":             {"at least one address is required", "至少需要一个地址"},
	"err.noBootstrap":        {"at least one bootstrap address is required", "至少需要一个 bootstrap 地址"},
	"err.allUnreachable":     {"none of the bootstrap addresses are reachable", "所有 bootstrap 地址均不可达"},
	"err.connectFailed":      {"connection failed, see the diagnostic report", "连接失败，请查看诊断报告"},
	"changeset.unsupported":  {"changesets support Kafka and RabbitMQ, not %s", "变更集目前支持 Kafka 和 RabbitMQ，不支持 %s"},
	"changeset.stale":        {"the instance changed since you planned; plan again and review the result", "生成计划后实例有变化，请重新 Plan 并确认"},
	"changeset.needDelete":   {"the plan deletes resources; allow deletions to apply it", "计划中包含删除操作，需要勾选「允许删除」才能执行"},
	"dlg.openChangeset":      {"Open changeset", "打开变更集"},
	"dlg.saveChangeset":      {"Save changeset", "保存变更集"},
	"kafka.skipNoCommit":     {"group %s has no committed offset on partition %d, so there is nothing stuck to skip; use Reset offsets instead", "消费组 %s 在分区 %d 上没有已提交的 offset，无法判断卡住的位置，请改用「重置 offset」"},
	"kafka.skipTooMany":      {"one skip can park at most %d messages", "单次最多跳过 %d 条消息"},
	"kafka.skipNothing":      {"nothing to skip: the group is already at the end of the selected partitions", "没有可跳过的消息：所选分区已消费到末尾"},
	"kafka.skipCommitFailed": {"the messages were saved (batch %[2]s) but moving the offsets failed: %[1]s", "消息已保存（批次 %[2]s），但移动 offset 失败：%[1]s"},
	"kafka.replayAgain":      {"this batch was already replayed on %s to %s; confirm to replay it again", "该批次已于 %s 回放到 %s，确认后可再次回放"},
	"kafka.replayPartition":  {"topic %s has %d partitions, but a message came from partition %d; replay by key instead", "主题 %s 只有 %d 个分区，但有消息来自分区 %d，请改为按 key 分区回放"},
	"cs.fewerPartitions":     {"topic %s has %d partitions, more than the %d asked for; Kafka cannot remove partitions", "主题 %s 已有 %d 个分区，多于期望的 %d 个；Kafka 不支持减少分区"},
	"cs.replication":         {"topic %s has replication factor %d, not %d; changing it needs a partition reassignment and is left alone", "主题 %s 的副本数是 %d 而非 %d；修改副本数需要分区重分配，变更集不会处理"},
	"cs.exchangeImmutable":   {"RabbitMQ cannot change an existing exchange; delete and recreate it by hand", "RabbitMQ 不支持修改已存在的 exchange，需手动删除后重建"},
	"cs.queueImmutable":      {"RabbitMQ cannot change an existing queue's type or arguments; use a policy, or recreate the queue", "RabbitMQ 不支持修改已存在队列的类型或参数，请改用 policy，或重建队列"},
	"cs.queueLoss":           {"%d messages will be lost", "队列中的 %d 条消息将丢失"},
	"err.readOnly":           {"instance %s is read-only, refused: %s", "实例 %s 为只读模式，已拒绝操作：%s"},
	"team.notConfigured":     {"no team repository is set; pick one in Team first", "还没有设置团队仓库，请先在「团队」里选择"},
	"dlg.teamRepo":           {"Pick the team repository (a Git working copy)", "选择团队仓库（Git 工作目录）"},
	"team.noIdentity":        {"git user.name / user.email is not set in this repository; commits need an author", "这个仓库没有设置 git user.name / user.email，提交需要作者信息"},
	"team.pushFailed":        {"saved locally, but pushing to the remote failed: %s", "已在本地保存，但推送到远端失败：%s"},
	"library.exists":         {"%s already exists in the library", "变更集库里已经有 %s"},
	"watch.kafka_lag":        {"Consumer lag", "消费积压"},
	"watch.rabbit_dlq":       {"Dead-letter queue", "死信队列"},
	"watch.redis_bigkey":     {"Big Redis key", "Redis 大 key"},
	"watch.recovered":        {"Recovered", "已恢复"},
	"watch.reminder":         {"Still firing", "仍在告警"},
	"watch.failed":           {"Check failed", "巡检失败"},
	"watch.more":             {"and %d more", "另有 %d 项"},
	"watch.backToNormal":     {"back under the threshold", "已回到阈值以内"},
	"watch.test":             {"Test notification", "测试通知"},
	"watch.testBody":         {"This is how MidDeck alerts look.", "MidDeck 的告警通知就是这个样子。"},
	"watch.off":              {"alerts only run in the desktop app", "巡检告警只在桌面应用中运行"},
	"ai.badChangeset":        {"the assistant drafted a changeset that is not valid", "助手生成的变更集格式不正确"},
	"err.wrongKind":          {"instance %s is not this middleware type", "实例 %s 不是该类型的中间件"},
	"err.storeUnavailable":   {"configuration store is unavailable", "配置存储不可用"},
	"err.unknownKind":        {"unknown kind %q", "未知类型 %q"},
	"err.needConfirm":        {"NEED_CONFIRM: this command may modify data or block the server", "NEED_CONFIRM: 该命令可能修改数据或阻塞服务，请确认后执行"},
	"compat.unavailable":     {"; unavailable: %s", "；不可用功能：%s"},
	"compat.tooOldHint":      {"Version is too old, read-only use is recommended.", "版本过旧，建议仅进行只读操作。"},

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
	"kafka.registry":     {"Schema Registry", "Schema Registry"},
	"kafka.registryOK":   {"%s · %d subjects", "%s，%d 个 subject"},
	"kafka.registryHint": {"Messages are still shown, but schema-encoded ones stay raw. Check the registry URL and credentials; it is reached through this connection's proxy / SSH hops.", "消息仍可查看，但 Schema 编码的消息无法解码。请检查 Registry 地址和凭据；它和本连接走同一个代理 / SSH 跳板。"},
	"schema.noRegistry":  {"%s has no Schema Registry configured. Add its URL in the connection settings.", "%s 没有配置 Schema Registry，请在连接设置中填写地址。"},
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
	"ai.noKey":              {"set an API key in AI settings first", "请先在 AI 设置中填写 API Key"},
	"ai.noModel":            {"set a model in AI settings first", "请先在 AI 设置中填写模型"},
	"ai.noAction":           {"the model did not propose an action, try rephrasing", "模型没有给出可执行的动作，换个说法试试"},
	"ai.unknownInstance":    {"the model picked an instance that does not exist (%s)", "模型选择了不存在的实例（%s）"},
	"ai.badProxy":           {"invalid proxy URL", "代理地址无效"},
	"ai.badKey":             {"the provider rejected the API key", "服务商拒绝了该 API Key"},
	"ai.notFound":           {"endpoint or model not found, check the base URL and model name", "接口或模型不存在，请检查 Base URL 和模型名"},
	"ai.rateLimited":        {"rate limited by the provider, try again shortly", "触发了服务商的限流，请稍后再试"},
	"ai.apiError":           {"the provider returned an error", "服务商返回错误"},
	"ai.unreachable":        {"cannot reach the AI provider", "无法连接 AI 服务"},
	"ai.viaProxy":           {"via proxy %s", "经由代理 %s"},
	"ai.direct":             {"direct connection, no proxy applies; if your browser uses a proxy, enter it in the Proxy field", "直连，没有可用的代理；如果浏览器走了代理，请把它填到“代理”一栏"},
	"ai.refused":            {"the model declined this request", "模型拒绝了这个请求"},
	"ai.badOutput":          {"could not read the model's answer", "无法解析模型的回复"},
	"ai.testOK":             {"connected, the model answered with %s in %d ms", "连接成功，模型以 %s 作答，耗时 %d ms"},
	"ai.disabled":           {"the AI assistant is turned off, enable it in AI settings", "AI 助手未开启，请在 AI 设置中开启"},
	"bastion.notFound":      {"jump host %s no longer exists, pick another one in the connection's network settings", "跳板机 %s 已不存在，请在连接的网络设置里重新选择"},
	"bastion.required":      {"name, host and user are required", "名称、主机和用户名为必填项"},
	"bastion.inUse":         {"still used by: %s", "仍有连接在使用：%s"},
	"share.imported":        {" (imported)", "（导入）"},
}
