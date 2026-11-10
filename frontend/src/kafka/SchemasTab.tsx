import { useCallback, useEffect, useState } from 'react'
import { App, Button, Drawer, Empty, Input, Select, Space, Table, Tag, Typography } from 'antd'
import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { api, errMsg } from '../api'
import { prettyJSON } from '../common'
import { useT } from '../settings'
import type { Instance, RegistrySchema, SchemaSubject } from '../types'

const typeColor: Record<string, string> = { AVRO: 'blue', PROTOBUF: 'purple', JSON: 'gold' }

/** Read-only browser for the Schema Registry of a Kafka connection. */
export default function SchemasTab({ inst }: { inst: Instance }) {
  const { message } = App.useApp()
  const t = useT()
  const [subjects, setSubjects] = useState<SchemaSubject[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [q, setQ] = useState('')
  const [open, setOpen] = useState<SchemaSubject | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setSubjects(await api.kafkaSchemaSubjects(inst.id))
    } catch (e) {
      message.error(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [inst.id, message])

  useEffect(() => {
    load()
  }, [load])

  const shown = (subjects ?? []).filter((s) => s.name.toLowerCase().includes(q.trim().toLowerCase()))
  return (
    <>
      <div className="toolbar">
        <Input prefix={<SearchOutlined />} placeholder={t('Filter subjects')} allowClear value={q} onChange={(e) => setQ(e.target.value)} style={{ width: 280 }} />
        <span className="muted">{inst.registry?.url}</span>
        <div className="spacer" />
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>{t('Refresh')}</Button>
      </div>
      <Table
        size="middle"
        rowKey="name"
        loading={loading}
        dataSource={shown}
        pagination={{ pageSize: 50, hideOnSinglePage: true }}
        onRow={(r) => ({ onClick: () => setOpen(r), style: { cursor: 'pointer' } })}
        locale={{ emptyText: <Empty description={t('No subjects')} /> }}
        columns={[
          { title: 'Subject', dataIndex: 'name', render: (n) => <span className="mono">{n}</span> },
          { title: t('Type'), dataIndex: 'type', width: 120, render: (x) => x && <Tag bordered={false} color={typeColor[x]}>{x}</Tag> },
          { title: t('Latest version'), dataIndex: 'latest', width: 130 },
          { title: t('Versions'), dataIndex: 'versions', width: 100, render: (v: number[] | null) => v?.length ?? 0 },
          { title: 'ID', dataIndex: 'id', width: 90 },
        ]}
      />
      <SchemaDrawer inst={inst} subject={open} onClose={() => setOpen(null)} />
    </>
  )
}

function SchemaDrawer({ inst, subject, onClose }: { inst: Instance; subject: SchemaSubject | null; onClose: () => void }) {
  const { message } = App.useApp()
  const t = useT()
  const [version, setVersion] = useState<number>()
  const [schema, setSchema] = useState<RegistrySchema | null>(null)

  useEffect(() => {
    setVersion(subject?.latest)
    setSchema(null)
  }, [subject])

  useEffect(() => {
    if (!subject || !version) return
    api.kafkaSchema(inst.id, subject.name, String(version)).then(setSchema).catch((e) => message.error(errMsg(e)))
  }, [inst.id, subject, version, message])

  const text = schema ? (schema.schemaType === 'PROTOBUF' ? schema.schema : prettyJSON(schema.schema)) : ''
  return (
    <Drawer title={<span className="mono">{subject?.name}</span>} width={720} open={!!subject} onClose={onClose} destroyOnClose>
      <Space style={{ marginBottom: 12 }} wrap>
        <Select
          style={{ width: 160 }}
          value={version}
          onChange={setVersion}
          options={[...(subject?.versions ?? [])].reverse().map((v) => ({ value: v, label: t('Version {n}', { n: v }) }))}
        />
        {schema && <Tag bordered={false} color={typeColor[schema.schemaType]}>{schema.schemaType}</Tag>}
        {schema && <span className="muted">ID {schema.id}</span>}
      </Space>
      {!!schema?.references?.length && (
        <div style={{ marginBottom: 12 }}>
          <div className="muted" style={{ marginBottom: 4 }}>{t('References')}</div>
          {schema.references.map((r) => <Tag key={r.name} bordered={false}>{r.name} → {r.subject} v{r.version}</Tag>)}
        </div>
      )}
      <div className="code-block">
        <Typography.Paragraph copyable={{ text: schema?.schema ?? '' }} style={{ marginBottom: 0 }}>
          <pre className="mono" style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{text}</pre>
        </Typography.Paragraph>
      </div>
    </Drawer>
  )
}
