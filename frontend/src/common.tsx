import { useState } from 'react'
import { Alert, Input, List, Modal, Space, Tag, Typography } from 'antd'
import { CheckCircleFilled, CloseCircleFilled, ExclamationCircleFilled, MinusCircleFilled } from '@ant-design/icons'
import type { CompatResult, Instance, Report } from './types'

export const kindLabel: Record<string, string> = { kafka: 'Kafka', redis: 'Redis', rabbitmq: 'RabbitMQ' }
export const kindColor: Record<string, string> = { kafka: 'geekblue', redis: 'red', rabbitmq: 'orange' }

export const envColor = (env: string) =>
  ({ prod: 'red', production: 'red', staging: 'orange', test: 'blue', dev: 'green' })[env] ?? 'default'

export function EnvTag({ env }: { env: string }) {
  return <Tag color={envColor(env)}>{env || 'default'}</Tag>
}

const statusIcon = {
  ok: <CheckCircleFilled style={{ color: '#2f9e44' }} />,
  warn: <ExclamationCircleFilled style={{ color: '#f08c00' }} />,
  fail: <CloseCircleFilled style={{ color: '#e03131' }} />,
  skip: <MinusCircleFilled style={{ color: '#adb5bd' }} />,
}

export function DiagReport({ report }: { report?: Report | null }) {
  if (!report) return null
  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      <Alert
        type={report.ok ? (report.steps.some((s) => s.status === 'warn') ? 'warning' : 'success') : 'error'}
        message={report.ok ? '连接测试通过' : '连接测试失败'}
        showIcon
      />
      <List
        size="small"
        bordered
        dataSource={report.steps}
        renderItem={(s) => (
          <List.Item>
            <Space direction="vertical" size={0} style={{ width: '100%' }}>
              <Space>
                {statusIcon[s.status]}
                <Typography.Text strong>{s.name}</Typography.Text>
                {s.durationMs > 0 && <Typography.Text type="secondary">{s.durationMs}ms</Typography.Text>}
              </Space>
              {s.detail && <Typography.Text type="secondary" style={{ paddingLeft: 22 }}>{s.detail}</Typography.Text>}
              {s.hint && <Typography.Text type="warning" style={{ paddingLeft: 22 }}>💡 {s.hint}</Typography.Text>}
            </Space>
          </List.Item>
        )}
      />
    </Space>
  )
}

const levelTag: Record<string, [string, string]> = {
  tested: ['green', '已实测'],
  supported: ['blue', '支持'],
  partial: ['gold', '部分支持'],
  unsupported: ['red', '不支持'],
  unknown: ['default', '版本未知'],
}

export function CompatBadge({ c }: { c?: CompatResult }) {
  if (!c) return null
  const [color, label] = levelTag[c.level] ?? levelTag.unknown
  return (
    <Tag color={color} title={c.message + (c.unavailable?.length ? '\n不可用：' + c.unavailable.join('、') : '')}>
      {c.product} {c.version || '?'} · {label}
    </Tag>
  )
}

/**
 * Confirms a dangerous operation. On production instances the user must type the
 * resource name, similar to deleting a GitHub repository.
 */
export function useDangerConfirm(inst: Instance) {
  const [modal, ctx] = Modal.useModal()
  const confirm = (title: string, resource: string, onOk: () => Promise<unknown>) => {
    const prod = inst.env === 'prod' || inst.env === 'production'
    let typed = ''
    modal.confirm({
      title,
      okType: 'danger',
      okText: '确认执行',
      width: 480,
      content: (
        <Space direction="vertical" style={{ width: '100%' }}>
          <Typography.Text>
            实例：<b>{inst.name}</b> <EnvTag env={inst.env} />
          </Typography.Text>
          <Typography.Text>
            对象：<Typography.Text code>{resource}</Typography.Text>
          </Typography.Text>
          {prod && (
            <>
              <Alert type="error" showIcon message="这是生产环境，请输入对象名称确认" />
              <Input placeholder={resource} onChange={(e) => (typed = e.target.value)} />
            </>
          )}
        </Space>
      ),
      onOk: () => {
        if (prod && typed !== resource) {
          return Promise.reject(new Error('名称不匹配'))
        }
        return onOk()
      },
    })
  }
  return { confirm, ctx }
}

export function CopyCommand({ cmd }: { cmd: string }) {
  return (
    <Alert
      type="info"
      message="等价命令"
      description={
        <Typography.Paragraph copyable={{ text: cmd }} className="mono" style={{ marginBottom: 0 }}>
          {cmd}
        </Typography.Paragraph>
      }
    />
  )
}

export function useToggle(init = false): [boolean, () => void, (v: boolean) => void] {
  const [v, set] = useState(init)
  return [v, () => set((x) => !x), set]
}

export const fmtTime = (ms: number) => (ms ? new Date(ms).toLocaleString() : '-')

export const prettyJSON = (s: string) => {
  const t = s.trim()
  if (!(t.startsWith('{') || t.startsWith('['))) return s
  try {
    return JSON.stringify(JSON.parse(t), null, 2)
  } catch {
    return s
  }
}
