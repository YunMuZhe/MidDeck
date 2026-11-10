import { useEffect, useState } from 'react'
import {
  App, AutoComplete, Button, Checkbox, Col, Divider, Drawer, Form, Input, InputNumber, Row, Segmented, Select, Space, Tabs, Tag, Typography,
} from 'antd'
import { ApiOutlined, CodeOutlined, CloudServerOutlined, DeleteOutlined, EditOutlined, LockOutlined, PlusOutlined, ProfileOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { api, errMsg } from './api'
import { BastionForm, hopSummary } from './Bastions'
import { DiagReport, KindIcon } from './common'
import { useT } from './settings'
import { emptyInstance, type Bastion, type BastionView, type Instance, type Kind, type Report, type SSHHop } from './types'

interface Props {
  open: boolean
  initial?: Instance | null
  projects: string[]
  onClose: () => void
  onSaved: (i: Instance) => void
}

type FormValues = Omit<Instance, 'network'> & {
  network: Omit<Instance['network'], 'addrMap'> & { addrMapText: string }
}

const toForm = (i: Instance): FormValues => ({
  ...i,
  registry: { url: '', username: '', password: '', insecureSkipVerify: false, ...i.registry },
  proto: { paths: i.proto?.paths ?? [], bindings: i.proto?.bindings ?? [] },
  network: {
    proxy: i.network.proxy ?? { type: '', addr: '' },
    ssh: (i.network.ssh ?? []).map((h) => ({ ...h, bastionId: h.bastionId ?? '' })),
    addrMapText: Object.entries(i.network.addrMap ?? {}).map(([k, v]) => `${k} => ${v}`).join('\n'),
  },
})

const fromForm = (v: FormValues, base: Instance): Instance => {
  const addrMap: Record<string, string> = {}
  for (const line of (v.network.addrMapText ?? '').split('\n')) {
    const [from, to] = line.split('=>').map((s) => s.trim())
    if (from && to) addrMap[from] = to
  }
  return {
    ...base,
    ...v,
    tags: v.tags ?? [],
    aliases: v.aliases ?? [],
    addrs: v.addrs ?? [],
    auth: { ...base.auth, ...v.auth },
    tls: { ...base.tls, ...v.tls },
    registry: { ...base.registry, ...v.registry, url: (v.registry?.url ?? '').trim() },
    // Bindings are edited from the Messages tab; keep them.
    proto: { bindings: base.proto?.bindings ?? [], paths: (v.proto?.paths ?? []).map((p) => p.trim()).filter(Boolean) },
    network: {
      proxy: v.network.proxy ?? { type: '', addr: '' },
      ssh: (v.network.ssh ?? []).map((h) => (h.bastionId ? ({ bastionId: h.bastionId } as SSHHop) : { ...h, bastionId: undefined })),
      addrMap,
    },
  }
}

const addrHint: Record<Kind, string> = {
  kafka: 'Bootstrap servers, e.g. kafka-1:9092 (press Enter to add more)',
  redis: 'Node address, e.g. 127.0.0.1:6379; add several for cluster or sentinel',
  rabbitmq: 'AMQP address (optional, used for connectivity checks), e.g. mq-1:5672',
}

export default function InstanceForm({ open, initial, projects, onClose, onSaved }: Props) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm<FormValues>()
  const [base, setBase] = useState<Instance>(emptyInstance())
  const [report, setReport] = useState<Report | null>(null)
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)
  const kind = (Form.useWatch('kind', form) as Kind | undefined) ?? 'kafka'
  const redisMode = Form.useWatch('redisMode', form)
  const mech = Form.useWatch(['auth', 'mechanism'], form)
  const tlsOn = Form.useWatch(['tls', 'enabled'], form)
  const proxyType = Form.useWatch(['network', 'proxy', 'type'], form)
  const hops = (Form.useWatch(['network', 'ssh'], form) ?? []).length
  const [bastions, setBastions] = useState<BastionView[]>([])
  const [bastionEdit, setBastionEdit] = useState<{ hop: number; initial: Bastion | null } | null>(null)
  const loadBastions = () => api.listBastions().then(setBastions).catch(() => {})
  const openBastion = (hop: number, initial: Bastion | null) => setBastionEdit({ hop, initial })

  useEffect(() => {
    if (!open) return
    const b = initial ?? emptyInstance()
    setBase(b)
    setReport(null)
    form.resetFields()
    form.setFieldsValue(toForm(b))
    loadBastions()
  }, [open, initial, form])

  const current = () => fromForm(form.getFieldsValue(true) as FormValues, base)

  const test = async () => {
    setTesting(true)
    setReport(null)
    try {
      setReport(await api.testConnection(current()))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setTesting(false)
    }
  }

  const save = async () => {
    await form.validateFields()
    setSaving(true)
    try {
      const saved = await api.saveInstance(current())
      message.success(t('Saved'))
      onSaved(saved)
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setSaving(false)
    }
  }

  const keepHint = base.id ? t('Saved — leave empty to keep') : ''

  const general = (
    <>
      <Form.Item name="kind" label={t('Type')}>
        <Segmented
          disabled={!!base.id}
          options={(['kafka', 'redis', 'rabbitmq'] as Kind[]).map((k) => ({
            value: k,
            label: <Space size={6} style={{ padding: '2px 4px' }}><KindIcon kind={k} size={16} />{{ kafka: 'Kafka', redis: 'Redis', rabbitmq: 'RabbitMQ' }[k]}</Space>,
          }))}
        />
      </Form.Item>
      <Row gutter={12}>
        <Col span={12}>
          <Form.Item name="name" label={t('Name')} rules={[{ required: true, message: t('Required') }]}>
            <Input placeholder={t('e.g. Orders Kafka')} />
          </Form.Item>
        </Col>
        <Col span={6}>
          <Form.Item name="project" label={t('Project')}>
            <AutoComplete options={projects.map((p) => ({ value: p }))} placeholder="order-service"
              filterOption={(input, opt) => (opt?.value ?? '').toLowerCase().includes(input.toLowerCase())} />
          </Form.Item>
        </Col>
        <Col span={6}>
          <Form.Item name="env" label={t('Environment')}>
            <Select options={['dev', 'test', 'staging', 'prod'].map((v) => ({ value: v }))} />
          </Form.Item>
        </Col>
      </Row>
      <Form.Item name="addrs" label={t('Addresses')} rules={kind === 'rabbitmq' ? [] : [{ required: true, message: t('Required') }]}>
        <Select mode="tags" tokenSeparators={[',', ' ', '\n']} placeholder={t(addrHint[kind])} open={false} />
      </Form.Item>
      {kind === 'redis' && (
        <Row gutter={12}>
          <Col span={10}>
            <Form.Item name="redisMode" label={t('Mode')}>
              <Segmented block options={[{ value: 'standalone', label: t('Standalone') }, { value: 'cluster', label: t('Cluster') }, { value: 'sentinel', label: t('Sentinel') }]} />
            </Form.Item>
          </Col>
          <Col span={4}>
            <Form.Item name="redisDb" label="DB">
              <InputNumber min={0} max={255} disabled={redisMode === 'cluster'} style={{ width: '100%' }} />
            </Form.Item>
          </Col>
          {redisMode === 'sentinel' && (
            <Col span={10}>
              <Form.Item name="masterSet" label={t('Master name')} rules={[{ required: true, message: t('Required') }]}>
                <Input placeholder="mymaster" />
              </Form.Item>
            </Col>
          )}
        </Row>
      )}
      {kind === 'rabbitmq' && (
        <Row gutter={12}>
          <Col span={16}>
            <Form.Item name="mgmtUrl" label={t('Management API')} rules={[{ required: true, message: t('Required') }]}>
              <Input placeholder="http://mq-1:15672" />
            </Form.Item>
          </Col>
          <Col span={8}>
            <Form.Item name="vhost" label="VHost"><Input placeholder="/" /></Form.Item>
          </Col>
        </Row>
      )}
      <Row gutter={12}>
        <Col span={12}>
          <Form.Item name="aliases" label={t('Aliases')} tooltip={t('Used by search (and later by the AI assistant) to find this instance')}>
            <Select mode="tags" tokenSeparators={[',']} open={false} placeholder={t('Press Enter to add')} />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="tags" label={t('Tags')}>
            <Select mode="tags" tokenSeparators={[',']} open={false} placeholder={t('Press Enter to add')} />
          </Form.Item>
        </Col>
      </Row>
      <Form.Item name="description" label={t('Description')}><Input.TextArea rows={2} /></Form.Item>
      <Form.Item name="readOnly" valuePropName="checked" style={{ marginBottom: 0 }}>
        <Checkbox>{t('Read-only (refuse all write operations)')}</Checkbox>
      </Form.Item>
    </>
  )

  const auth = (
    <>
      {kind === 'kafka' && (
        <Form.Item name={['auth', 'mechanism']} label={t('SASL mechanism')}>
          <Segmented options={['none', 'plain', 'scram-sha-256', 'scram-sha-512'].map((v) => ({ value: v, label: v === 'none' ? t('None') : v.toUpperCase() }))} />
        </Form.Item>
      )}
      {(kind !== 'kafka' || (mech && mech !== 'none')) && (
        <Row gutter={12}>
          <Col span={12}>
            <Form.Item name={['auth', 'username']} label={t('Username')} tooltip={kind === 'redis' ? t('Redis 6+ ACL user; leave empty to use password only') : undefined}>
              <Input autoComplete="off" />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item name={['auth', 'password']} label={t('Password')} tooltip={t('Stored in the system keychain')}>
              <Input.Password autoComplete="new-password" placeholder={keepHint} />
            </Form.Item>
          </Col>
        </Row>
      )}
      <Divider orientation="left" plain>TLS</Divider>
      <Space size="large">
        <Form.Item name={['tls', 'enabled']} valuePropName="checked" noStyle><Checkbox>{t('Enable TLS')}</Checkbox></Form.Item>
        {tlsOn && (
          <Form.Item name={['tls', 'insecureSkipVerify']} valuePropName="checked" noStyle>
            <Checkbox>{t('Skip certificate verification (test only)')}</Checkbox>
          </Form.Item>
        )}
      </Space>
      {tlsOn && (
        <div style={{ marginTop: 14 }}>
          <Form.Item name={['tls', 'caFile']} label={t('CA certificate path')}><Input placeholder="/path/to/ca.pem" /></Form.Item>
          <Row gutter={12}>
            <Col span={12}><Form.Item name={['tls', 'certFile']} label={t('Client certificate')}><Input /></Form.Item></Col>
            <Col span={12}><Form.Item name={['tls', 'keyFile']} label={t('Client key')}><Input /></Form.Item></Col>
          </Row>
        </div>
      )}
    </>
  )

  const regUrl = Form.useWatch(['registry', 'url'], form)
  const decoding = (
    <>
      <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
        {t('Decode binary messages into readable JSON. Schema Registry is used automatically for messages in the Confluent wire format; plain Protobuf topics use local .proto files bound per topic from the Messages tab.')}
      </Typography.Paragraph>
      <Divider orientation="left" plain>Schema Registry</Divider>
      <Form.Item
        name={['registry', 'url']}
        label={t('Registry URL')}
        extra={t('Confluent Schema Registry or a compatible API (Karapace, Redpanda, Apicurio ccompat). Reached through this connection\'s proxy / SSH hops.')}
        rules={[{ pattern: /^\s*(https?:\/\/\S+)?\s*$/, message: t('Must start with http:// or https://') }]}
      >
        <Input placeholder="http://schema-registry:8081" autoComplete="off" />
      </Form.Item>
      {!!regUrl?.trim() && (
        <>
          <Row gutter={12}>
            <Col span={12}><Form.Item name={['registry', 'username']} label={t('Username / API key')}><Input autoComplete="off" /></Form.Item></Col>
            <Col span={12}>
              <Form.Item name={['registry', 'password']} label={t('Password / API secret')} tooltip={t('Stored in the system keychain')}>
                <Input.Password autoComplete="new-password" placeholder={keepHint} />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name={['registry', 'insecureSkipVerify']} valuePropName="checked">
            <Checkbox>{t('Skip certificate verification (test only)')}</Checkbox>
          </Form.Item>
        </>
      )}
      <Divider orientation="left" plain>{t('Protobuf files')}</Divider>
      <Form.Item
        name={['proto', 'paths']}
        label={t('.proto files or directories')}
        extra={t('Directories are searched recursively and used as import roots. Press Enter to add more.')}
      >
        <Select mode="tags" open={false} suffixIcon={null} placeholder="~/work/acme-protos" tokenSeparators={[',']} />
      </Form.Item>
    </>
  )

  const network = (
    <>
      <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
        {t('Reach intranet middleware through SSH jump hosts (chained in order) or a SOCKS5 proxy. Every connection is opened from the last hop, so internal broker addresses work too.')}
      </Typography.Paragraph>
      <Form.List name={['network', 'ssh']}>
        {(fields, { add, remove }) => (
          <>
            {fields.map((f, idx) => (
              <div key={f.key} className="code-block" style={{ marginBottom: 10, paddingBottom: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10 }}>
                  <Typography.Text strong style={{ whiteSpace: 'nowrap' }}>{t('Hop {n}', { n: idx + 1 })}</Typography.Text>
                  <Form.Item name={[f.name, 'bastionId']} noStyle>
                    <Select
                      style={{ flex: 1, minWidth: 0 }}
                      options={[
                        ...bastions.map((b) => ({ value: b.id, label: <Space><CloudServerOutlined />{b.name}<span className="muted mono">{hopSummary(b)}</span></Space> })),
                        { value: '', label: t('Custom (this connection only)') },
                      ]}
                      dropdownRender={(menu) => (
                        <>
                          {menu}
                          <Divider style={{ margin: '4px 0' }} />
                          <Button type="text" block icon={<PlusOutlined />} style={{ textAlign: 'left' }} onClick={() => openBastion(f.name, null)}>{t('New jump host')}</Button>
                        </>
                      )}
                    />
                  </Form.Item>
                  <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => remove(f.name)} />
                </div>
                <Form.Item noStyle shouldUpdate>
                  {() => {
                    const hop = (form.getFieldValue(['network', 'ssh', f.name]) ?? {}) as SSHHop
                    const shared = hop.bastionId ? bastions.find((b) => b.id === hop.bastionId) : undefined
                    if (hop.bastionId) {
                      return (
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, paddingBottom: 12 }}>
                          {shared ? (
                            <>
                              <span className="mono">{hopSummary(shared)}</span>
                              <Tag bordered={false}>{shared.authType === 'agent' ? 'ssh-agent' : shared.authType === 'key' ? t('Private key') : t('Password')}</Tag>
                              <span className="muted">{t('Shared by {n} connection(s)', { n: shared.usedBy?.length ?? 0 })}</span>
                              <Button size="small" type="link" icon={<EditOutlined />} onClick={() => openBastion(f.name, shared)} style={{ marginLeft: 'auto' }}>{t('Edit jump host')}</Button>
                            </>
                          ) : (
                            <Typography.Text type="danger">{t('This jump host was deleted; pick another one.')}</Typography.Text>
                          )}
                        </div>
                      )
                    }
                    const a = hop.authType
                    return (
                      <Row gutter={8}>
                        <Col span={10}><Form.Item name={[f.name, 'host']} label={t('Host')} rules={[{ required: true, message: t('Required') }]}><Input /></Form.Item></Col>
                        <Col span={4}><Form.Item name={[f.name, 'port']} label={t('Port')}><InputNumber min={1} max={65535} placeholder="22" style={{ width: '100%' }} /></Form.Item></Col>
                        <Col span={10}><Form.Item name={[f.name, 'user']} label={t('User')} rules={[{ required: true, message: t('Required') }]}><Input /></Form.Item></Col>
                        <Col span={8}>
                          <Form.Item name={[f.name, 'authType']} label={t('Auth')}>
                            <Select options={[{ value: 'password', label: t('Password') }, { value: 'key', label: t('Private key') }, { value: 'agent', label: 'ssh-agent' }]} />
                          </Form.Item>
                        </Col>
                        {a === 'key' && <Col span={8}><Form.Item name={[f.name, 'keyPath']} label={t('Key path')}><Input placeholder="~/.ssh/id_rsa" /></Form.Item></Col>}
                        {a !== 'agent' && (
                          <Col span={8}>
                            <Form.Item name={[f.name, 'secret']} label={a === 'key' ? t('Key passphrase') : t('Password')}>
                              <Input.Password autoComplete="new-password" placeholder={keepHint} />
                            </Form.Item>
                          </Col>
                        )}
                        <Col span={24} style={{ marginTop: -12, paddingBottom: 10 }}>
                          <Button size="small" type="link" style={{ padding: 0 }} icon={<CloudServerOutlined />}
                            onClick={() => openBastion(f.name, { ...hop, id: '', name: hop.host ?? '', bastionId: undefined } as Bastion)}>
                            {t('Save as shared jump host')}
                          </Button>
                        </Col>
                      </Row>
                    )
                  }}
                </Form.Item>
              </div>
            ))}
            <Button type="dashed" block icon={<PlusOutlined />}
              onClick={() => add(bastions.length ? { bastionId: bastions[0].id } : { bastionId: '', host: '', port: 22, user: '', authType: 'key', keyPath: '~/.ssh/id_rsa' })}>
              {t('Add jump host')}
            </Button>
          </>
        )}
      </Form.List>
      <Divider orientation="left" plain>{t('Proxy')}</Divider>
      <Row gutter={12}>
        <Col span={8}>
          <Form.Item name={['network', 'proxy', 'type']}>
            <Select options={[{ value: '', label: t('No proxy') }, { value: 'socks5', label: 'SOCKS5' }]} />
          </Form.Item>
        </Col>
        {proxyType === 'socks5' && (
          <>
            <Col span={16}><Form.Item name={['network', 'proxy', 'addr']}><Input placeholder="127.0.0.1:1080" /></Form.Item></Col>
            <Col span={12}><Form.Item name={['network', 'proxy', 'user']} label={t('Proxy user')}><Input /></Form.Item></Col>
            <Col span={12}><Form.Item name={['network', 'proxy', 'secret']} label={t('Proxy password')}><Input.Password /></Form.Item></Col>
          </>
        )}
      </Row>
      <Divider orientation="left" plain>{t('Address mapping')}</Divider>
      <Form.Item
        name={['network', 'addrMapText']}
        extra={t('One per line: original => replacement. Use it when brokers or cluster nodes advertise addresses you cannot reach.')}
      >
        <Input.TextArea rows={3} className="mono" placeholder="10.0.1.5:9092 => kafka-1.internal:9092" />
      </Form.Item>
    </>
  )

  return (
    <Drawer
      title={base.id ? t('Edit {name}', { name: base.name }) : t('New connection')}
      width={760}
      open={open}
      onClose={onClose}
      destroyOnClose
      footer={
        <div style={{ display: 'flex', gap: 8 }}>
          <Button icon={<ThunderboltOutlined />} onClick={test} loading={testing}>{t('Test connection')}</Button>
          <div style={{ flex: 1 }} />
          <Button onClick={onClose}>{t('Cancel')}</Button>
          <Button type="primary" onClick={save} loading={saving}>{t('Save')}</Button>
        </div>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Tabs
          items={[
            { key: 'basic', label: <span><ProfileOutlined /> {t('General')}</span>, children: general, forceRender: true },
            { key: 'auth', label: <span><LockOutlined /> {t('Auth & TLS')}</span>, children: auth, forceRender: true },
            { key: 'net', label: <span><ApiOutlined /> {t('Network')}{hops ? ` · ${hops} SSH` : ''}</span>, children: network, forceRender: true },
            ...(kind === 'kafka' ? [{ key: 'decode', label: <span><CodeOutlined /> {t('Decoding')}</span>, children: decoding, forceRender: true }] : []),
          ]}
        />
      </Form>
      {(testing || report) && (
        <>
          <Divider orientation="left">{t('Diagnostics')}</Divider>
          {testing ? <Typography.Text type="secondary">{t('Checking network → auth → version → permissions…')}</Typography.Text> : <DiagReport report={report} />}
        </>
      )}
      <BastionForm
        open={!!bastionEdit}
        initial={bastionEdit?.initial}
        onClose={() => setBastionEdit(null)}
        onSaved={async (b) => {
          const hop = bastionEdit?.hop
          setBastionEdit(null)
          await loadBastions()
          if (hop !== undefined) form.setFieldValue(['network', 'ssh', hop], { bastionId: b.id })
        }}
      />
    </Drawer>
  )
}
