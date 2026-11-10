import { useCallback, useEffect, useState } from 'react'
import { Tabs } from 'antd'
import { CheckCircleFilled, ExclamationCircleFilled } from '@ant-design/icons'
import { api } from '../api'
import { num, Tiles } from '../common'
import { useT } from '../settings'
import type { ActionArgs, GroupSummary, Instance, Intent, Topic } from '../types'
import TopicsTab from './TopicsTab'
import MessagesTab from './MessagesTab'
import GroupsTab from './GroupsTab'
import ParkedTab from './Parked'
import SchemasTab from './SchemasTab'

export default function KafkaView({ inst, instances, intent, onIntentDone }: { inst: Instance; instances: Instance[]; intent?: Intent; onIntentDone: () => void }) {
  const t = useT()
  const [tab, setTab] = useState('topics')
  const [createPrefill, setCreatePrefill] = useState<ActionArgs>()
  const [searchPrefill, setSearchPrefill] = useState<ActionArgs>()
  const [resetPrefill, setResetPrefill] = useState<ActionArgs>()
  const [parkedFocus, setParkedFocus] = useState<string>()

  // Proposals from the AI assistant: open the matching form, filled in, never run it.
  useEffect(() => {
    if (!intent) return
    const a: ActionArgs = { ...intent.args, seq: intent.seq }
    switch (intent.action) {
      case 'open_instance':
        if (['topics', 'messages', 'groups', 'parked', 'schemas'].includes(a.tab)) setTab(a.tab)
        break
      case 'kafka_create_topic':
        setTab('topics')
        setCreatePrefill(a)
        break
      case 'kafka_search_messages':
        setTab('messages')
        setSearchPrefill(a)
        break
      case 'kafka_reset_offsets':
        setTab('groups')
        setResetPrefill(a)
        break
    }
    onIntentDone()
  }, [intent, onIntentDone])
  const [msgTopic, setMsgTopic] = useState<{ name: string; n: number }>()
  const [topics, setTopics] = useState<Topic[] | null>(null)
  const [groups, setGroups] = useState<GroupSummary[] | null>(null)

  const refreshStats = useCallback(() => {
    api.kafkaTopics(inst.id).then(setTopics).catch(() => {})
    api.kafkaGroups(inst.id).then(setGroups).catch(() => {})
  }, [inst.id])

  useEffect(() => {
    refreshStats()
  }, [refreshStats])

  const userTopics = topics?.filter((x) => !x.internal)
  const lag = groups?.reduce((s, g) => s + g.lag, 0)
  const lagging = groups?.filter((g) => g.lag > 0).length ?? 0
  const stable = groups?.filter((g) => g.state === 'Stable').length ?? 0

  return (
    <>
      <Tiles
        items={[
          { label: t('Topics'), value: num(userTopics?.length), note: topics ? t('{n} internal', { n: topics.length - (userTopics?.length ?? 0) }) : undefined },
          { label: t('Partitions'), value: num(userTopics?.reduce((s, x) => s + x.partitions, 0)) },
          { label: t('Consumer groups'), value: num(groups?.length), note: groups ? t('{n} active', { n: stable }) : undefined },
          {
            label: t('Total lag'),
            value: num(lag),
            note:
              lag === undefined ? undefined : lag > 0 ? (
                <><ExclamationCircleFilled style={{ color: '#f08c00' }} /> {t('{n} group(s) behind', { n: lagging })}</>
              ) : (
                <><CheckCircleFilled style={{ color: '#2f9e44' }} /> {t('All caught up')}</>
              ),
          },
        ]}
      />
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          { key: 'topics', label: t('Topics'), children: <TopicsTab inst={inst} prefill={createPrefill} onBrowse={(name) => { setMsgTopic({ name, n: Date.now() }); setTab('messages') }} onChanged={refreshStats} /> },
          { key: 'messages', label: t('Messages'), children: <MessagesTab inst={inst} topic={msgTopic} prefill={searchPrefill} /> },
          { key: 'groups', label: t('Consumer groups'), children: <GroupsTab inst={inst} onChanged={refreshStats} resetPrefill={resetPrefill} onParked={(b) => { setParkedFocus(b.id); setTab('parked') }} /> },
          { key: 'parked', label: t('Parked'), children: <ParkedTab inst={inst} instances={instances} focus={parkedFocus} /> },
          ...(inst.registry?.url ? [{ key: 'schemas', label: t('Schemas'), children: <SchemasTab inst={inst} /> }] : []),
        ]}
      />
    </>
  )
}

/** Equivalent CLI commands, shown next to forms so users can copy them to a server. */
export const kafkaCli = {
  bootstrap: (inst: Instance) => (inst.addrs ?? []).join(','),
  createTopic: (inst: Instance, name: string, p: number, r: number, cfg: Record<string, string>) =>
    `kafka-topics.sh --bootstrap-server ${kafkaCli.bootstrap(inst)} --create --topic ${name || '<topic>'} --partitions ${p} --replication-factor ${r}` +
    Object.entries(cfg).map(([k, v]) => ` --config ${k}=${v}`).join(''),
  resetOffsets: (inst: Instance, group: string, topic: string, mode: string, value: number) => {
    const m: Record<string, string> = {
      earliest: '--to-earliest',
      latest: '--to-latest',
      offset: `--to-offset ${value}`,
      shift: `--shift-by ${value}`,
      timestamp: `--to-datetime ${new Date(value || Date.now()).toISOString().replace('Z', '')}`,
    }
    return `kafka-consumer-groups.sh --bootstrap-server ${kafkaCli.bootstrap(inst)} --group ${group} --topic ${topic} --reset-offsets ${m[mode] ?? ''} --execute`
  },
}
