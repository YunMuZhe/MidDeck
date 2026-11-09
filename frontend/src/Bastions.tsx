import { useEffect, useState } from 'react'
import { App, Button, Col, Divider, Empty, Form, Input, InputNumber, Modal, Row, Select, Space, Table, Tag, Tooltip, Typography } from 'antd'
import { CloudServerOutlined, DeleteOutlined, EditOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { api, errMsg } from './api'
import { DiagReport } from './common'
import { useT } from './settings'
import type { Bastion, BastionView, Report } from './types'

export const emptyBastion = (): Bastion => ({ id: '', name: '', host: '', port: 22, user: '', authType: 'key', keyPath: '~/.ssh/id_rsa' })

export const hopSummary = (b: { user: string; host: string; port?: number }) => `${b.user}@${b.host}:${b.port || 22}`

/** Create or edit one shared jump host. */
export function BastionForm({ open, initial, onClose, onSaved }: {
  open: boolean
  initial?: Bastion | null
  onClose: () => void
  onSaved: (b: Bastion) => void
}) {
  const { message } = App.useApp()
  const t = useT()
  const [form] = Form.useForm<Bastion>()
  const [report, setReport] = useState<Report | null>(null)
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)
  const authType = Form.useWatch('authType', form)
  const editing = !!initial?.id
  const hasSecret = (initial as BastionView | undefined)?.hasSecret

  useEffect(() => {
    if (!open) return
    setReport(null)
    form.resetFields()
    form.setFieldsValue({ ...emptyBastion(), ...initial })
  }, [open, initial, form])

  const values = () => ({ ...emptyBastion(), ...initial, ...form.getFieldsValue(true) }) as Bastion

  const test = async () => {
    await form.validateFields(['host', 'user'])
    setTesting(true)
    setReport(null)
    try {
      setReport(await api.testBastion(values()))
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
      const b = await api.saveBastion(values())
      message.success(t('Saved'))
      onSaved(b)
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title={editing ? t('Edit jump host') : t('New jump host')}
      open={open}
      onCancel={onClose}
      width={640}
      destroyOnClose
      footer={
        <div style={{ display: 'flex', gap: 8 }}>
          <Button icon={<ThunderboltOutlined />} onClick={test} loading={testing}>{t('Test login')}</Button>
          <div style={{ flex: 1 }} />
          <Button onClick={onClose}>{t('Cancel')}</Button>
          <Button type="primary" onClick={save} loading={saving}>{t('Save')}</Button>
        </div>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Row gutter={12}>
          <Col span={12}>
            <Form.Item name="name" label={t('Name')} rules={[{ required: true, message: t('Required') }]}>
              <Input placeholder={t('e.g. Prod bastion (Beijing)')} />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item name="description" label={t('Description')}><Input /></Form.Item>
          </Col>
          <Col span={12}><Form.Item name="host" label={t('Host')} rules={[{ required: true, message: t('Required') }]}><Input /></Form.Item></Col>
          <Col span={4}><Form.Item name="port" label={t('Port')}><InputNumber min={1} max={65535} placeholder="22" style={{ width: '100%' }} /></Form.Item></Col>
          <Col span={8}><Form.Item name="user" label={t('User')} rules={[{ required: true, message: t('Required') }]}><Input /></Form.Item></Col>
          <Col span={8}>
            <Form.Item name="authType" label={t('Auth')}>
              <Select options={[{ value: 'password', label: t('Password') }, { value: 'key', label: t('Private key') }, { value: 'agent', label: 'ssh-agent' }]} />
            </Form.Item>
          </Col>
          {authType === 'key' && <Col span={8}><Form.Item name="keyPath" label={t('Key path')}><Input placeholder="~/.ssh/id_rsa" /></Form.Item></Col>}
          {authType !== 'agent' && (
            <Col span={8}>
              <Form.Item name="secret" label={authType === 'key' ? t('Key passphrase') : t('Password')} tooltip={t('Stored in the system keychain')}>
                <Input.Password autoComplete="new-password" placeholder={editing && hasSecret ? t('Saved — leave empty to keep') : ''} />
              </Form.Item>
            </Col>
          )}
        </Row>
      </Form>
      {(testing || report) && (
        <>
          <Divider orientation="left">{t('Diagnostics')}</Divider>
          {testing ? <Typography.Text type="secondary">{t('Logging in…')}</Typography.Text> : <DiagReport report={report} />}
        </>
      )}
    </Modal>
  )
}

/** Manage the shared jump host list. */
export function BastionsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { message, modal } = App.useApp()
  const t = useT()
  const [rows, setRows] = useState<BastionView[]>([])
  const [editing, setEditing] = useState<Bastion | null>(null)
  const [formOpen, setFormOpen] = useState(false)

  const reload = () => api.listBastions().then(setRows).catch((e) => message.error(errMsg(e)))
  useEffect(() => {
    if (open) reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const remove = (b: BastionView) =>
    modal.confirm({
      title: t('Delete jump host {name}?', { name: b.name }),
      okText: t('Delete'),
      okButtonProps: { danger: true },
      cancelText: t('Cancel'),
      onOk: async () => {
        try {
          await api.deleteBastion(b.id)
          reload()
        } catch (e) {
          message.error(errMsg(e))
        }
      },
    })

  return (
    <Modal title={t('Jump hosts')} open={open} onCancel={onClose} footer={null} width={860}>
      <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
        {t('Maintain bastions once and pick them in any connection. Changing one here updates every connection that uses it.')}
      </Typography.Paragraph>
      <div className="toolbar">
        <div className="spacer" />
        <Button type="primary" icon={<PlusOutlined />} onClick={() => { setEditing(null); setFormOpen(true) }}>{t('New jump host')}</Button>
      </div>
      <Table
        size="middle"
        rowKey="id"
        dataSource={rows}
        pagination={false}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('No jump hosts yet')} /> }}
        columns={[
          { title: t('Name'), dataIndex: 'name', render: (n, r) => <Space align="start"><CloudServerOutlined className="muted" style={{ marginTop: 4 }} /><div><div>{n}</div>{r.description && <div className="muted" style={{ fontSize: 12 }}>{r.description}</div>}</div></Space> },
          { title: t('Address'), render: (_, r) => <span className="mono" style={{ whiteSpace: 'nowrap' }}>{hopSummary(r)}</span> },
          { title: t('Auth'), dataIndex: 'authType', width: 110, render: (a) => <Tag bordered={false}>{a === 'agent' ? 'ssh-agent' : a === 'key' ? t('Private key') : t('Password')}</Tag> },
          {
            title: t('Used by'), dataIndex: 'usedBy', width: 150, render: (u: string[] | null) =>
              u?.length ? <Tooltip title={u.join(', ')}><span style={{ whiteSpace: 'nowrap' }}>{t('{n} connection(s)', { n: u.length })}</span></Tooltip> : <span className="muted">—</span>,
          },
          {
            title: '', width: 90, align: 'right', render: (_, r) => (
              <Space size={0}>
                <Button type="text" size="small" icon={<EditOutlined />} onClick={() => { setEditing(r); setFormOpen(true) }} />
                <Tooltip title={r.usedBy?.length ? t('In use, cannot delete') : undefined}>
                  <Button type="text" size="small" danger icon={<DeleteOutlined />} disabled={!!r.usedBy?.length} onClick={() => remove(r)} />
                </Tooltip>
              </Space>
            ),
          },
        ]}
      />
      <BastionForm open={formOpen} initial={editing} onClose={() => setFormOpen(false)} onSaved={() => { setFormOpen(false); reload() }} />
    </Modal>
  )
}
