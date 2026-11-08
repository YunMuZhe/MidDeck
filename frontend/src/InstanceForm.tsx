import { useEffect, useState } from 'react'
import { App, AutoComplete, Button, Checkbox, Col, Divider, Drawer, Form, Input, InputNumber, Radio, Row, Select, Space, Tabs, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { api, errMsg } from './api'
import { DiagReport } from './common'
import { emptyInstance, type Instance, type Kind, type Report } from './types'

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
  network: {
    proxy: i.network.proxy ?? { type: '', addr: '' },
    ssh: i.network.ssh ?? [],
    addrMapText: Object.entries(i.network.addrMap ?? {})
      .map(([k, v]) => `${k} => ${v}`)
      .join('\n'),
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
    network: { proxy: v.network.proxy ?? { type: '', addr: '' }, ssh: v.network.ssh ?? [], addrMap },
  }
}

const addrHint: Record<Kind, string> = {
  kafka: 'Bootstrap 地址，如 kafka-1:9092（可填多个）',
  redis: '节点地址，如 127.0.0.1:6379；集群/哨兵可填多个',
  rabbitmq: 'AMQP 地址（可选，仅用于连通性检查），如 mq-1:5672',
}

export default function InstanceForm({ open, initial, projects, onClose, onSaved }: Props) {
  const { message } = App.useApp()
  const [form] = Form.useForm<FormValues>()
  const [base, setBase] = useState<Instance>(emptyInstance())
  const [report, setReport] = useState<Report | null>(null)
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)
  const kind = Form.useWatch('kind', form) as Kind | undefined
  const redisMode = Form.useWatch('redisMode', form)
  const mech = Form.useWatch(['auth', 'mechanism'], form)
  const tlsOn = Form.useWatch(['tls', 'enabled'], form)
  const proxyType = Form.useWatch(['network', 'proxy', 'type'], form)

  useEffect(() => {
    if (!open) return
    const b = initial ?? emptyInstance()
    setBase(b)
    setReport(null)
    form.resetFields()
    form.setFieldsValue(toForm(b))
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
      message.success('已保存')
      onSaved(saved)
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setSaving(false)
    }
  }

  const basic = (
    <>
      <Row gutter={12}>
        <Col span={12}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="订单服务 Kafka" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="kind" label="类型">
            <Radio.Group optionType="button" disabled={!!base.id}>
              <Radio value="kafka">Kafka</Radio>
              <Radio value="redis">Redis</Radio>
              <Radio value="rabbitmq">RabbitMQ</Radio>
            </Radio.Group>
          </Form.Item>
        </Col>
      </Row>
      <Row gutter={12}>
        <Col span={12}>
          <Form.Item name="project" label="项目 / 分组">
            <AutoComplete options={projects.map((p) => ({ value: p }))} placeholder="例如 order-service"
              filterOption={(input, opt) => (opt?.value ?? '').toLowerCase().includes(input.toLowerCase())} />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="env" label="环境">
            <Select options={['dev', 'test', 'staging', 'prod'].map((v) => ({ value: v }))} />
          </Form.Item>
        </Col>
      </Row>
      <Form.Item name="addrs" label="地址" tooltip={addrHint[kind ?? 'kafka']} rules={kind === 'rabbitmq' ? [] : [{ required: true }]}>
        <Select mode="tags" tokenSeparators={[',', ' ', '\n']} placeholder={addrHint[kind ?? 'kafka']} open={false} />
      </Form.Item>
      {kind === 'redis' && (
        <Row gutter={12}>
          <Col span={8}>
            <Form.Item name="redisMode" label="模式">
              <Select options={[{ value: 'standalone', label: '单机' }, { value: 'cluster', label: '集群' }, { value: 'sentinel', label: '哨兵' }]} />
            </Form.Item>
          </Col>
          <Col span={8}>
            <Form.Item name="redisDb" label="DB">
              <InputNumber min={0} max={255} disabled={redisMode === 'cluster'} style={{ width: '100%' }} />
            </Form.Item>
          </Col>
          {redisMode === 'sentinel' && (
            <Col span={8}>
              <Form.Item name="masterSet" label="Master 名称" rules={[{ required: true }]}>
                <Input placeholder="mymaster" />
              </Form.Item>
            </Col>
          )}
        </Row>
      )}
      {kind === 'rabbitmq' && (
        <Row gutter={12}>
          <Col span={16}>
            <Form.Item name="mgmtUrl" label="Management API" rules={[{ required: true }]}>
              <Input placeholder="http://mq-1:15672" />
            </Form.Item>
          </Col>
          <Col span={8}>
            <Form.Item name="vhost" label="VHost">
              <Input placeholder="/" />
            </Form.Item>
          </Col>
        </Row>
      )}
      <Form.Item name="aliases" label="别名" tooltip="用于搜索与 AI 定位实例，例如：订单MQ、order-kafka">
        <Select mode="tags" tokenSeparators={[',']} open={false} placeholder="回车添加" />
      </Form.Item>
      <Form.Item name="tags" label="标签">
        <Select mode="tags" tokenSeparators={[',']} open={false} placeholder="回车添加" />
      </Form.Item>
      <Form.Item name="description" label="描述">
        <Input.TextArea rows={2} />
      </Form.Item>
      <Form.Item name="readOnly" valuePropName="checked">
        <Checkbox>只读模式（拒绝所有写操作）</Checkbox>
      </Form.Item>
    </>
  )

  const auth = (
    <>
      {kind === 'kafka' && (
        <Form.Item name={['auth', 'mechanism']} label="SASL 机制">
          <Select options={['none', 'plain', 'scram-sha-256', 'scram-sha-512'].map((v) => ({ value: v }))} />
        </Form.Item>
      )}
      {(kind !== 'kafka' || (mech && mech !== 'none')) && (
        <Row gutter={12}>
          <Col span={12}>
            <Form.Item name={['auth', 'username']} label="用户名" tooltip={kind === 'redis' ? 'Redis 6+ ACL 用户，留空则仅使用密码' : undefined}>
              <Input autoComplete="off" />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item name={['auth', 'password']} label="密码" tooltip="保存在系统钥匙串中；编辑时留空表示不修改">
              <Input.Password autoComplete="new-password" placeholder={base.id ? '已保存（留空不修改）' : ''} />
            </Form.Item>
          </Col>
        </Row>
      )}
      <Divider orientation="left" plain>TLS</Divider>
      <Space>
        <Form.Item name={['tls', 'enabled']} valuePropName="checked" noStyle>
          <Checkbox>启用 TLS</Checkbox>
        </Form.Item>
        {tlsOn && (
          <Form.Item name={['tls', 'insecureSkipVerify']} valuePropName="checked" noStyle>
            <Checkbox>跳过证书校验（仅测试环境）</Checkbox>
          </Form.Item>
        )}
      </Space>
      {tlsOn && (
        <div style={{ marginTop: 12 }}>
          <Form.Item name={['tls', 'caFile']} label="CA 证书路径"><Input placeholder="/path/to/ca.pem" /></Form.Item>
          <Row gutter={12}>
            <Col span={12}><Form.Item name={['tls', 'certFile']} label="客户端证书"><Input /></Form.Item></Col>
            <Col span={12}><Form.Item name={['tls', 'keyFile']} label="客户端私钥"><Input /></Form.Item></Col>
          </Row>
        </div>
      )}
    </>
  )

  const network = (
    <>
      <Typography.Paragraph type="secondary">
        通过 SSH 跳板（可多级）或 SOCKS5 代理访问内网中间件。所有连接都从最后一跳发起，Kafka broker 通告的内网地址也能正常访问。
      </Typography.Paragraph>
      <Row gutter={12}>
        <Col span={8}>
          <Form.Item name={['network', 'proxy', 'type']} label="代理">
            <Select options={[{ value: '', label: '不使用' }, { value: 'socks5', label: 'SOCKS5' }]} />
          </Form.Item>
        </Col>
        {proxyType === 'socks5' && (
          <>
            <Col span={16}>
              <Form.Item name={['network', 'proxy', 'addr']} label="代理地址"><Input placeholder="127.0.0.1:1080" /></Form.Item>
            </Col>
            <Col span={12}><Form.Item name={['network', 'proxy', 'user']} label="代理用户"><Input /></Form.Item></Col>
            <Col span={12}><Form.Item name={['network', 'proxy', 'secret']} label="代理密码"><Input.Password /></Form.Item></Col>
          </>
        )}
      </Row>
      <Divider orientation="left" plain>SSH 跳板（按顺序连接）</Divider>
      <Form.List name={['network', 'ssh']}>
        {(fields, { add, remove }) => (
          <>
            {fields.map((f, idx) => (
              <div key={f.key} style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: '12px 12px 0', marginBottom: 12 }}>
                <Space style={{ marginBottom: 8, justifyContent: 'space-between', width: '100%' }}>
                  <Typography.Text strong>跳板 {idx + 1}</Typography.Text>
                  <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => remove(f.name)} />
                </Space>
                <Row gutter={8}>
                  <Col span={10}><Form.Item name={[f.name, 'host']} label="主机" rules={[{ required: true }]}><Input /></Form.Item></Col>
                  <Col span={4}><Form.Item name={[f.name, 'port']} label="端口"><InputNumber min={1} max={65535} placeholder="22" style={{ width: '100%' }} /></Form.Item></Col>
                  <Col span={10}><Form.Item name={[f.name, 'user']} label="用户" rules={[{ required: true }]}><Input /></Form.Item></Col>
                  <Col span={8}>
                    <Form.Item name={[f.name, 'authType']} label="认证">
                      <Select options={[{ value: 'password', label: '密码' }, { value: 'key', label: '私钥' }, { value: 'agent', label: 'ssh-agent' }]} />
                    </Form.Item>
                  </Col>
                  <Form.Item noStyle shouldUpdate>
                    {() => {
                      const t = form.getFieldValue(['network', 'ssh', f.name, 'authType'])
                      return (
                        <>
                          {t === 'key' && (
                            <Col span={8}><Form.Item name={[f.name, 'keyPath']} label="私钥路径"><Input placeholder="~/.ssh/id_rsa" /></Form.Item></Col>
                          )}
                          {t !== 'agent' && (
                            <Col span={8}>
                              <Form.Item name={[f.name, 'secret']} label={t === 'key' ? '私钥口令' : '密码'}>
                                <Input.Password autoComplete="new-password" placeholder={base.id ? '留空不修改' : ''} />
                              </Form.Item>
                            </Col>
                          )}
                        </>
                      )
                    }}
                  </Form.Item>
                </Row>
              </div>
            ))}
            <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add({ host: '', port: 22, user: '', authType: 'key', keyPath: '~/.ssh/id_rsa' })}>
              添加跳板
            </Button>
          </>
        )}
      </Form.List>
      <Divider orientation="left" plain>地址映射</Divider>
      <Form.Item name={['network', 'addrMapText']} tooltip="把服务端通告的不可达地址改写为可达地址，常用于 Kafka advertised.listeners 和 Redis 集群节点" label="每行一条：原地址 => 目标地址">
        <Input.TextArea rows={3} className="mono" placeholder={'10.0.1.5:9092 => kafka-1.internal:9092'} />
      </Form.Item>
    </>
  )

  return (
    <Drawer
      title={base.id ? `编辑：${base.name}` : '新建连接'}
      width={720}
      open={open}
      onClose={onClose}
      destroyOnClose
      extra={
        <Space>
          <Button onClick={test} loading={testing}>测试连接</Button>
          <Button type="primary" onClick={save} loading={saving}>保存</Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark="optional">
        <Tabs
          items={[
            { key: 'basic', label: '基本信息', children: basic, forceRender: true },
            { key: 'auth', label: '认证与 TLS', children: auth, forceRender: true },
            { key: 'net', label: `网络${(form.getFieldValue(['network', 'ssh'])?.length ?? 0) > 0 ? ' · SSH' : ''}`, children: network, forceRender: true },
          ]}
        />
      </Form>
      {(testing || report) && (
        <>
          <Divider orientation="left">诊断报告</Divider>
          {testing ? <Typography.Text type="secondary">正在逐层检测：网络 → 认证 → 版本 → 权限…</Typography.Text> : <DiagReport report={report} />}
        </>
      )}
    </Drawer>
  )
}
