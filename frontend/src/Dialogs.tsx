import { useEffect, useState } from 'react'
import { App, Alert, Checkbox, Input, Modal, Radio, Space, Table, Tag, Typography } from 'antd'
import { api, errMsg } from './api'
import { EnvTag, fmtTime, KindIcon } from './common'
import { useT } from './settings'
import type { AuditEntry, Instance, Matrix } from './types'

export function ImportModal({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [pass, setPass] = useState('')
  const [policy, setPolicy] = useState('skip')
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    try {
      const r = await api.importInstances(pass, policy)
      if (r.added + r.overwritten + r.skipped > 0) {
        message.success(t('Added {a}, overwritten {o}, skipped {s}', { a: r.added, o: r.overwritten, s: r.skipped }))
        onDone()
        onClose()
      }
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal title={t('Import connections')} open={open} onCancel={onClose} onOk={run} okText={t('Choose file & import')} cancelText={t('Cancel')} confirmLoading={busy}>
      <Space direction="vertical" style={{ width: '100%' }} size={12}>
        <div>
          <div className="muted" style={{ marginBottom: 4 }}>{t('Passphrase (only for encrypted bundles)')}</div>
          <Input.Password value={pass} onChange={(e) => setPass(e.target.value)} />
        </div>
        <div>
          <div className="muted" style={{ marginBottom: 4 }}>{t('When a connection already exists')}</div>
          <Radio.Group value={policy} onChange={(e) => setPolicy(e.target.value)}>
            <Radio value="skip">{t('Skip')}</Radio>
            <Radio value="overwrite">{t('Overwrite')}</Radio>
            <Radio value="copy">{t('Import as a copy')}</Radio>
          </Radio.Group>
        </div>
      </Space>
    </Modal>
  )
}

export function ExportModal({ open, instances, onClose }: { open: boolean; instances: Instance[]; onClose: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [ids, setIds] = useState<string[]>([])
  const [withSecrets, setWithSecrets] = useState(false)
  const [pass, setPass] = useState('')
  useEffect(() => {
    if (open) setIds(instances.map((i) => i.id))
  }, [open, instances])
  const run = async () => {
    if (withSecrets && pass.length < 8) {
      message.warning(t('Passphrase must be at least 8 characters'))
      return
    }
    try {
      const path = await api.exportInstances(ids, withSecrets ? pass : '')
      if (path) {
        message.success(t('Exported to {path}', { path }))
        onClose()
      }
    } catch (e) {
      message.error(errMsg(e))
    }
  }
  return (
    <Modal title={t('Export connections')} open={open} onCancel={onClose} onOk={run} okText={t('Export')} cancelText={t('Cancel')} width={680} okButtonProps={{ disabled: ids.length === 0 }}>
      <Table
        size="small"
        rowKey="id"
        pagination={false}
        scroll={{ y: 300 }}
        rowSelection={{ selectedRowKeys: ids, onChange: (k) => setIds(k as string[]) }}
        dataSource={instances}
        columns={[
          { title: t('Name'), dataIndex: 'name', render: (n, r) => <Space><KindIcon kind={r.kind} size={16} />{n}</Space> },
          { title: t('Project'), dataIndex: 'project' },
          { title: t('Environment'), dataIndex: 'env', render: (e) => <EnvTag env={e} /> },
        ]}
      />
      <Space direction="vertical" style={{ width: '100%', marginTop: 14 }}>
        <Checkbox checked={withSecrets} onChange={(e) => setWithSecrets(e.target.checked)}>
          {t('Include passwords (encrypted with a passphrase, AES-256-GCM)')}
        </Checkbox>
        {withSecrets ? (
          <Input.Password value={pass} onChange={(e) => setPass(e.target.value)} placeholder={t('At least 8 characters; share it through another channel')} />
        ) : (
          <Alert type="info" showIcon message={t('Passwords are not exported by default; teammates fill them in after importing.')} />
        )}
      </Space>
    </Modal>
  )
}

export function AuditModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const t = useT()
  const [rows, setRows] = useState<AuditEntry[]>([])
  useEffect(() => {
    if (open) api.auditLog(500).then(setRows).catch(() => setRows([]))
  }, [open])
  return (
    <Modal title={t('Audit log (this machine)')} open={open} onCancel={onClose} footer={null} width={980}>
      <Table
        size="small"
        rowKey={(r) => r.time + r.action}
        dataSource={rows}
        pagination={{ pageSize: 15 }}
        columns={[
          { title: t('Time'), dataIndex: 'time', width: 170, render: (v) => fmtTime(new Date(v).getTime()) },
          { title: t('Instance'), dataIndex: 'name', render: (n, r) => <Space>{n}<EnvTag env={r.env} /></Space> },
          { title: t('Action'), dataIndex: 'action', render: (a) => <Tag bordered={false}>{a}</Tag> },
          { title: t('Details'), dataIndex: 'detail', render: (d) => <span className="mono cell-ellipsis" style={{ maxWidth: 320 }}>{JSON.stringify(d)}</span> },
          { title: t('Result'), dataIndex: 'error', render: (e) => (e ? <Tag color="error" title={e}>{t('Failed')}</Tag> : <Tag color="success">{t('OK')}</Tag>) },
        ]}
      />
    </Modal>
  )
}

export function AboutModal({ open, version, onClose }: { open: boolean; version: string; onClose: () => void }) {
  const t = useT()
  const [matrix, setMatrix] = useState<Record<string, Matrix>>({})
  useEffect(() => {
    if (open) api.compatMatrix().then(setMatrix).catch(() => {})
  }, [open])
  return (
    <Modal title={t('About MidDeck')} open={open} onCancel={onClose} footer={null} width={760}>
      <Typography.Paragraph>
        {t('Version')} <Tag bordered={false}>{version}</Tag> · <a href="https://github.com/YunMuZhe/MidDeck" target="_blank" rel="noreferrer">GitHub</a>
      </Typography.Paragraph>
      <Typography.Title level={5}>{t('Version support')}</Typography.Title>
      <Table
        size="small"
        pagination={false}
        rowKey="product"
        dataSource={Object.values(matrix)}
        columns={[
          { title: t('Product'), dataIndex: 'product' },
          { title: t('Tested in CI'), dataIndex: 'tested', render: (v: string[]) => v.map((x) => <Tag color="success" bordered={false} key={x}>{x}</Tag>) },
          { title: t('Supported'), dataIndex: 'supportedMin', render: (v) => `≥ ${v}` },
          { title: t('Partial'), dataIndex: 'partialMin', render: (v) => `≥ ${v}` },
        ]}
        expandable={{
          expandedRowRender: (m: Matrix) => (
            <Space wrap>
              {m.features.filter((f) => f.min !== '0').map((f) => <Tag key={f.key} bordered={false}>{f.label} ≥ {f.min}</Tag>)}
            </Space>
          ),
        }}
      />
    </Modal>
  )
}
