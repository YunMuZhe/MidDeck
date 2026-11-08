import { useCallback, useEffect, useState } from 'react'
import { Alert, App, Button, Drawer, Result, Space, Spin, Tag, Typography } from 'antd'
import { DeleteOutlined, DisconnectOutlined, EditOutlined, LockOutlined, ReloadOutlined } from '@ant-design/icons'
import { api, errMsg } from './api'
import { CompatBadge, DiagReport, EnvTag, kindColor, kindLabel, useDangerConfirm } from './common'
import KafkaView from './kafka/KafkaView'
import RedisView from './redis/RedisView'
import RabbitView from './rabbitmq/RabbitView'
import type { ConnectResult, Instance, Report } from './types'

interface Props {
  inst: Instance
  onEdit: () => void
  onDeleted: () => void
}

export default function InstanceView({ inst, onEdit, onDeleted }: Props) {
  const { message } = App.useApp()
  const [state, setState] = useState<'connecting' | 'ok' | 'fail'>('connecting')
  const [conn, setConn] = useState<ConnectResult | null>(null)
  const [report, setReport] = useState<Report | null>(null)
  const [error, setError] = useState('')
  const [showReport, setShowReport] = useState(false)
  const { confirm, ctx } = useDangerConfirm(inst)

  const connect = useCallback(async (fresh = false) => {
    setState('connecting')
    setError('')
    try {
      if (fresh) await api.disconnect(inst.id)
      const r = await api.connect(inst.id)
      setConn(r)
      if (r.report) setReport(r.report)
      setState('ok')
    } catch (e) {
      setError(errMsg(e))
      setState('fail')
    }
  }, [inst.id])

  useEffect(() => {
    connect()
  }, [connect])

  // Connect() returns the report on failure via a rejected promise without data,
  // so run a diagnostic test to show the user what went wrong.
  const diagnose = async () => {
    setShowReport(true)
    setReport(null)
    try {
      setReport(await api.testConnection(await api.getInstance(inst.id)))
    } catch (e) {
      message.error(errMsg(e))
    }
  }

  const prod = inst.env === 'prod' || inst.env === 'production'
  const compat = conn?.info.compat

  return (
    <div className={prod ? 'env-prod' : undefined} style={{ minHeight: '100%' }}>
      {ctx}
      <div style={{ padding: '14px 20px 0' }}>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Space wrap>
            <Typography.Title level={4} style={{ margin: 0 }}>{inst.name}</Typography.Title>
            <Tag color={kindColor[inst.kind]}>{kindLabel[inst.kind]}</Tag>
            <EnvTag env={inst.env} />
            {inst.readOnly && <Tag icon={<LockOutlined />}>只读</Tag>}
            {(inst.network?.ssh?.length ?? 0) > 0 && <Tag>SSH × {inst.network.ssh.length}</Tag>}
            {state === 'ok' && <CompatBadge c={compat} />}
          </Space>
          <Space>
            <Button size="small" icon={<ReloadOutlined />} onClick={() => connect(true)}>重连</Button>
            <Button size="small" onClick={() => (report ? setShowReport(true) : diagnose())}>诊断报告</Button>
            <Button size="small" icon={<EditOutlined />} onClick={onEdit}>编辑</Button>
            <Button
              size="small"
              danger
              icon={<DeleteOutlined />}
              onClick={() =>
                confirm('删除连接配置', inst.name, async () => {
                  await api.deleteInstance(inst.id)
                  onDeleted()
                })
              }
            />
          </Space>
        </Space>
        <Typography.Text type="secondary" className="mono">
          {(inst.addrs ?? []).join(', ') || inst.mgmtUrl}
        </Typography.Text>
        {state === 'ok' && compat && ['partial', 'unsupported', 'unknown'].includes(compat.level) && (
          <Alert
            style={{ marginTop: 10 }}
            type={compat.level === 'unsupported' ? 'error' : 'warning'}
            showIcon
            message={compat.message}
            description={compat.unavailable?.length ? '不可用功能：' + compat.unavailable.join('、') : undefined}
            closable
          />
        )}
        {state === 'ok' && compat?.unavailable?.length && !['partial', 'unsupported', 'unknown'].includes(compat.level) ? (
          <Alert style={{ marginTop: 10 }} type="info" showIcon closable message={'当前版本不可用：' + compat.unavailable.join('、')} />
        ) : null}
      </div>

      <div style={{ padding: '0 20px 20px' }}>
        {state === 'connecting' && <Spin tip="连接中…" style={{ display: 'block', marginTop: 120 }}><div /></Spin>}
        {state === 'fail' && (
          <Result
            status="warning"
            title="连接失败"
            subTitle={error}
            extra={
              <Space>
                <Button type="primary" onClick={diagnose}>查看诊断报告</Button>
                <Button onClick={() => connect(true)}>重试</Button>
                <Button onClick={onEdit}>修改配置</Button>
              </Space>
            }
          />
        )}
        {state === 'ok' && inst.kind === 'kafka' && <KafkaView inst={inst} />}
        {state === 'ok' && inst.kind === 'redis' && <RedisView inst={inst} />}
        {state === 'ok' && inst.kind === 'rabbitmq' && <RabbitView inst={inst} />}
      </div>

      <Drawer title="连接诊断报告" width={600} open={showReport} onClose={() => setShowReport(false)}
        extra={<Button size="small" icon={<DisconnectOutlined />} onClick={diagnose}>重新检测</Button>}>
        {report ? <DiagReport report={report} /> : <Spin tip="正在检测…"><div style={{ height: 80 }} /></Spin>}
      </Drawer>
    </div>
  )
}
