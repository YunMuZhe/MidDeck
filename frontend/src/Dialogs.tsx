import { useEffect, useState } from 'react'
import { App, Alert, Checkbox, Descriptions, Input, Modal, Radio, Space, Table, Tag, Typography } from 'antd'
import { api, errMsg } from './api'
import { EnvTag, kindLabel } from './common'
import type { AuditEntry, Instance, Matrix } from './types'

export function ImportModal({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: () => void }) {
  const { message } = App.useApp()
  const [pass, setPass] = useState('')
  const [policy, setPolicy] = useState('skip')
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    try {
      const r = await api.importInstances(pass, policy)
      if (r.added + r.overwritten + r.skipped > 0) {
        message.success(`新增 ${r.added}，覆盖 ${r.overwritten}，跳过 ${r.skipped}`)
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
    <Modal title="导入连接配置" open={open} onCancel={onClose} onOk={run} okText="选择文件并导入" confirmLoading={busy}>
      <Space direction="vertical" style={{ width: '100%' }}>
        <Typography.Text>口令（文件加密导出时需要）</Typography.Text>
        <Input.Password value={pass} onChange={(e) => setPass(e.target.value)} placeholder="未加密的文件留空" />
        <Typography.Text>ID 冲突时</Typography.Text>
        <Radio.Group value={policy} onChange={(e) => setPolicy(e.target.value)}>
          <Radio value="skip">跳过</Radio>
          <Radio value="overwrite">覆盖</Radio>
          <Radio value="copy">作为副本导入</Radio>
        </Radio.Group>
      </Space>
    </Modal>
  )
}

export function ExportModal({ open, instances, onClose }: { open: boolean; instances: Instance[]; onClose: () => void }) {
  const { message } = App.useApp()
  const [ids, setIds] = useState<string[]>([])
  const [withSecrets, setWithSecrets] = useState(false)
  const [pass, setPass] = useState('')
  useEffect(() => {
    if (open) setIds(instances.map((i) => i.id))
  }, [open, instances])
  const run = async () => {
    if (withSecrets && pass.length < 8) {
      message.warning('口令至少 8 位')
      return
    }
    try {
      const path = await api.exportInstances(ids, withSecrets ? pass : '')
      if (path) {
        message.success('已导出到 ' + path)
        onClose()
      }
    } catch (e) {
      message.error(errMsg(e))
    }
  }
  return (
    <Modal title="导出连接配置" open={open} onCancel={onClose} onOk={run} okText="导出" width={640} okButtonProps={{ disabled: ids.length === 0 }}>
      <Table
        size="small"
        rowKey="id"
        pagination={false}
        scroll={{ y: 300 }}
        rowSelection={{ selectedRowKeys: ids, onChange: (k) => setIds(k as string[]) }}
        dataSource={instances}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '类型', dataIndex: 'kind', render: (k) => kindLabel[k] },
          { title: '项目', dataIndex: 'project' },
          { title: '环境', dataIndex: 'env', render: (e) => <EnvTag env={e} /> },
        ]}
      />
      <Space direction="vertical" style={{ width: '100%', marginTop: 12 }}>
        <Checkbox checked={withSecrets} onChange={(e) => setWithSecrets(e.target.checked)}>
          包含密码（使用口令加密，AES-256-GCM）
        </Checkbox>
        {withSecrets ? (
          <Input.Password value={pass} onChange={(e) => setPass(e.target.value)} placeholder="至少 8 位，通过其他渠道告知同事" />
        ) : (
          <Alert type="info" showIcon message="默认不导出密码，同事导入后需自行填写。" />
        )}
      </Space>
    </Modal>
  )
}

export function AuditModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [rows, setRows] = useState<AuditEntry[]>([])
  useEffect(() => {
    if (open) api.auditLog(500).then(setRows).catch(() => setRows([]))
  }, [open])
  return (
    <Modal title="操作审计（本机）" open={open} onCancel={onClose} footer={null} width={960}>
      <Table
        size="small"
        rowKey={(r) => r.time + r.action}
        dataSource={rows}
        pagination={{ pageSize: 15 }}
        columns={[
          { title: '时间', dataIndex: 'time', width: 170, render: (t) => new Date(t).toLocaleString() },
          { title: '实例', dataIndex: 'name', render: (n, r) => <Space>{n}<EnvTag env={r.env} /></Space> },
          { title: '操作', dataIndex: 'action', render: (a) => <Tag>{a}</Tag> },
          { title: '详情', dataIndex: 'detail', render: (d) => <span className="mono cell-ellipsis" style={{ maxWidth: 300 }}>{JSON.stringify(d)}</span> },
          { title: '结果', dataIndex: 'error', render: (e) => (e ? <Tag color="red" title={e}>失败</Tag> : <Tag color="green">成功</Tag>) },
        ]}
      />
    </Modal>
  )
}

export function AboutModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [version, setVersion] = useState('')
  const [matrix, setMatrix] = useState<Record<string, Matrix>>({})
  useEffect(() => {
    if (!open) return
    api.version().then(setVersion).catch(() => {})
    api.compatMatrix().then(setMatrix).catch(() => {})
  }, [open])
  return (
    <Modal title="关于 MidDeck" open={open} onCancel={onClose} footer={null} width={720}>
      <Typography.Paragraph>
        版本 <Tag>{version}</Tag> · <a href="https://github.com/YunMuZhe/MidDeck" target="_blank" rel="noreferrer">GitHub</a>
      </Typography.Paragraph>
      <Typography.Title level={5}>版本支持矩阵</Typography.Title>
      {Object.entries(matrix).map(([k, m]) => (
        <Descriptions key={k} size="small" bordered column={1} title={m.product} style={{ marginBottom: 16 }}>
          <Descriptions.Item label="CI 实测">{m.tested.map((v) => <Tag color="green" key={v}>{v}</Tag>)}</Descriptions.Item>
          <Descriptions.Item label="完整支持">≥ {m.supportedMin}</Descriptions.Item>
          <Descriptions.Item label="部分支持">≥ {m.partialMin}</Descriptions.Item>
          <Descriptions.Item label="版本相关功能">
            {m.features.filter((f) => f.min !== '0').map((f) => <Tag key={f.key}>{f.label} ≥ {f.min}</Tag>)}
          </Descriptions.Item>
        </Descriptions>
      ))}
    </Modal>
  )
}
