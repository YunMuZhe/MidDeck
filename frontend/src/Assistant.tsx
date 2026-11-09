import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Alert, App, Button, Checkbox, Col, Descriptions, Empty, Form, Input, Modal, Row, Segmented, Space, Spin, Switch, Typography } from 'antd'
import { RobotOutlined, SendOutlined, SettingOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { api, errMsg } from './api'
import { EnvTag, KindIcon } from './common'
import { useT } from './settings'
import type { AIAction, AISettings, Instance } from './types'

const defaults: Record<string, { baseUrl: string; model: string }> = {
  anthropic: { baseUrl: '', model: 'claude-opus-5-5' },
  openai: { baseUrl: 'https://api.openai.com/v1', model: '' },
}

/** Provider, model and key for the assistant. */
export function AISettingsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm<AISettings>()
  const [hasKey, setHasKey] = useState(false)
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null)
  const provider = Form.useWatch('provider', form)

  useEffect(() => {
    if (!open) return
    setResult(null)
    api.getAISettings().then((s) => {
      setHasKey(s.hasKey)
      form.setFieldsValue({ ...s, apiKey: '' })
    }).catch((e) => message.error(errMsg(e)))
  }, [open, form, message])

  const values = () => form.getFieldsValue(true) as AISettings

  const test = async () => {
    setTesting(true)
    setResult(null)
    try {
      setResult({ ok: true, text: await api.testAI(values()) })
    } catch (e) {
      setResult({ ok: false, text: errMsg(e) })
    } finally {
      setTesting(false)
    }
  }

  const save = async () => {
    try {
      await api.saveAISettings(values())
      message.success(t('Saved'))
      onClose()
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  return (
    <Modal
      title={<Space><RobotOutlined />{t('AI assistant settings')}</Space>}
      open={open}
      onCancel={onClose}
      width={620}
      footer={
        <div style={{ display: 'flex', gap: 8 }}>
          <Button icon={<ThunderboltOutlined />} onClick={test} loading={testing}>{t('Test')}</Button>
          <div style={{ flex: 1 }} />
          <Button onClick={onClose}>{t('Cancel')}</Button>
          <Button type="primary" onClick={save}>{t('Save')}</Button>
        </div>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item name="enabled" valuePropName="checked" label={t('Enable the assistant')}>
          <Switch />
        </Form.Item>
        <Form.Item name="provider" label={t('Protocol')}>
          <Segmented
            options={[{ value: 'anthropic', label: 'Anthropic' }, { value: 'openai', label: t('OpenAI-compatible') }]}
            onChange={(v) => form.setFieldsValue(defaults[v as string])}
          />
        </Form.Item>
        <Row gutter={12}>
          <Col span={14}>
            <Form.Item name="baseUrl" label="Base URL" extra={provider === 'openai' ? t('OpenAI, DeepSeek, Qwen, Moonshot, Ollama, vLLM… any /chat/completions endpoint') : t('Leave empty for api.anthropic.com')}>
              <Input placeholder={provider === 'openai' ? 'https://api.openai.com/v1' : 'https://api.anthropic.com'} />
            </Form.Item>
          </Col>
          <Col span={10}>
            <Form.Item name="model" label={t('Model')}>
              <Input placeholder={provider === 'openai' ? 'deepseek-chat / qwen-plus / …' : 'claude-opus-5-5'} />
            </Form.Item>
          </Col>
        </Row>
        <Form.Item name="apiKey" label="API Key" tooltip={t('Stored in the system keychain')}>
          <Input.Password autoComplete="new-password" placeholder={hasKey ? t('Saved — leave empty to keep') : provider === 'openai' ? t('Optional for local models') : ''} />
        </Form.Item>
        <Form.Item name="proxy" label={t('Proxy (optional)')}>
          <Input placeholder="http://127.0.0.1:7890  /  socks5://127.0.0.1:1080" />
        </Form.Item>
        <Form.Item name="sendAddresses" valuePropName="checked" style={{ marginBottom: 8 }}>
          <Checkbox>{t('Also send instance addresses to the model')}</Checkbox>
        </Form.Item>
      </Form>
      <Alert type="info" showIcon message={t('The model sees connection names, projects, environments, tags, aliases and descriptions — never passwords. It only fills in forms; nothing runs until you confirm.')} />
      {result && <Alert style={{ marginTop: 12 }} type={result.ok ? 'success' : 'error'} showIcon message={result.text} />}
    </Modal>
  )
}

const examples = [
  'Create topic order-refund on the prod orders Kafka, 12 partitions, keep 7 days',
  'Skip the stuck messages for group billing-worker on payment-events',
  'Show messages containing "orderId":1042 in order-events from the last hour',
  'Find session keys for user 42 in the session cache',
  'Add a staging Redis at 10.0.3.7:6379 db 2 through the prod bastion',
]

/** ⌘K command bar: one sentence in, one proposed action out. */
export function AssistantModal({ open, instances, current, onClose, onApply, onSettings }: {
  open: boolean
  instances: Instance[]
  current: string | null
  onClose: () => void
  onApply: (a: AIAction) => void
  onSettings: () => void
}) {
  const t = useT()
  const [prompt, setPrompt] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<AIAction | null>(null)
  const [error, setError] = useState('')
  const [settings, setSettings] = useState<AISettings | null>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (!open) return
    setResult(null)
    setError('')
    api.getAISettings().then(setSettings).catch(() => setSettings(null))
    setTimeout(() => inputRef.current?.focus(), 50)
  }, [open])

  const ask = async () => {
    if (!prompt.trim()) return
    setBusy(true)
    setResult(null)
    setError('')
    try {
      setResult(await api.askAI(prompt.trim(), current ?? ''))
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setBusy(false)
    }
  }

  const byId = (id?: string) => instances.find((i) => i.id === id)
  const ready = settings?.enabled && (settings.hasKey || settings.provider === 'openai')

  return (
    <Modal open={open} onCancel={onClose} footer={null} width={680} closable={false} className="assistant-modal" destroyOnClose>
      <div className="assistant-input">
        <RobotOutlined className="assistant-icon" />
        <Input.TextArea
          ref={inputRef}
          autoSize={{ minRows: 1, maxRows: 4 }}
          variant="borderless"
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder={t('Describe what you want to do…')}
          onPressEnter={(e) => {
            if (!e.shiftKey) {
              e.preventDefault()
              ask()
            }
          }}
          disabled={!ready}
        />
        <Button type="primary" icon={<SendOutlined />} loading={busy} onClick={ask} disabled={!ready || !prompt.trim()} />
        <Button type="text" icon={<SettingOutlined />} onClick={onSettings} />
      </div>
      {!ready && settings && (
        <Alert
          type="info"
          showIcon
          message={t('Set up a model first: an Anthropic key, or any OpenAI-compatible endpoint (including local Ollama).')}
          action={<Button size="small" type="primary" onClick={onSettings}>{t('AI settings')}</Button>}
        />
      )}
      {busy && <div style={{ padding: 24, textAlign: 'center' }}><Spin /></div>}
      {error && <Alert type="error" showIcon message={error} />}
      {result && <Proposal a={result} byId={byId} onApply={() => onApply(result)} onOpen={(id) => onApply({ action: 'open_instance', args: { instance_id: id } })} />}
      {ready && !busy && !result && !error && (
        <div className="assistant-examples">
          <div className="muted" style={{ marginBottom: 6 }}>{t('For example')}</div>
          {examples.map((x) => (
            <div key={x} className="assistant-example" onClick={() => setPrompt(t(x))}>{t(x)}</div>
          ))}
        </div>
      )}
      <div className="muted assistant-foot">{t('Enter to send · Shift+Enter for a new line · results only prefill forms')}</div>
    </Modal>
  )
}

const actionTitle: Record<string, string> = {
  open_instance: 'Open connection',
  new_connection: 'New connection',
  kafka_create_topic: 'Create topic',
  kafka_search_messages: 'Search messages',
  kafka_reset_offsets: 'Reset offsets',
  redis_scan: 'Browse keys',
  redis_command: 'Redis command',
  rabbit_create_queue: 'Create queue',
  rabbit_publish: 'Publish message',
}

const argLabel: Record<string, string> = {
  tab: 'Tab', kind: 'Type', name: 'Name', project: 'Project', env: 'Environment', addrs: 'Addresses', redis_mode: 'Mode', redis_db: 'DB',
  mgmt_url: 'Management API', vhost: 'VHost', username: 'Username', bastion_id: 'Jump host', aliases: 'Aliases', description: 'Description',
  partitions: 'Partitions', replication: 'Replication factor', retention_ms: 'retention.ms', cleanup: 'cleanup.policy', topic: 'Topic',
  partition: 'Partition', mode: 'Mode', offset: 'Offset', since: 'Time', limit: 'Limit', key: 'Key', contains: 'Value contains',
  group: 'Consumer group', value: 'Value', pattern: 'Pattern', command: 'Command', type: 'Type', bind_exchange: 'Bind to exchange',
  routing_key: 'Routing key', exchange: 'Exchange', payload: 'Payload',
}

function Proposal({ a, byId, onApply, onOpen }: { a: AIAction; byId: (id?: string) => Instance | undefined; onApply: () => void; onOpen: (id: string) => void }) {
  const t = useT()
  const args = a.args ?? {}
  if (a.action === 'reply') {
    const cands: string[] = args.candidate_ids ?? []
    return (
      <div className="proposal">
        <Typography.Paragraph style={{ marginBottom: cands.length ? 10 : 0, whiteSpace: 'pre-wrap' }}>{args.message || a.text}</Typography.Paragraph>
        {cands.map((id) => {
          const i = byId(id)
          return i ? <InstanceChip key={id} inst={i} onClick={() => onOpen(id)} /> : null
        })}
      </div>
    )
  }
  const inst = byId(args.instance_id)
  const rows: { k: string; v: ReactNode }[] = Object.entries(args)
    .filter(([k, v]) => k !== 'instance_id' && v !== '' && v !== null && !(Array.isArray(v) && !v.length))
    .map(([k, v]) => ({ k, v: <span className="mono">{Array.isArray(v) ? v.join(', ') : String(v)}</span> }))
  return (
    <div className="proposal">
      <div className="proposal-head">
        <Typography.Text strong>{t(actionTitle[a.action] ?? a.action)}</Typography.Text>
        {inst && <InstanceChip inst={inst} />}
      </div>
      {a.text && <Typography.Paragraph type="secondary" style={{ marginBottom: 8 }}>{a.text}</Typography.Paragraph>}
      {rows.length > 0 ? (
        <Descriptions size="small" column={2} items={rows.map((r) => ({ key: r.k, label: t(argLabel[r.k] ?? r.k), children: r.v }))} />
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={false} style={{ margin: 0 }} />
      )}
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 12 }}>
        <Button type="primary" onClick={onApply}>{a.action === 'open_instance' ? t('Open') : t('Open prefilled form')}</Button>
      </div>
    </div>
  )
}

function InstanceChip({ inst, onClick }: { inst: Instance; onClick?: () => void }) {
  return (
    <span className={'inst-chip' + (onClick ? ' clickable' : '')} onClick={onClick}>
      <KindIcon kind={inst.kind} size={16} />
      <span>{inst.name}</span>
      <EnvTag env={inst.env} />
    </span>
  )
}
