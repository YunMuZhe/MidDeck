import { theme as antdTheme, type ThemeConfig } from 'antd'
import type { Kind } from './types'

/**
 * Middleware kinds are an identity (categorical) encoding. These are the first
 * three slots of the dataviz reference palette, which pass colorblind-safety
 * checks for every pair in both modes. Kinds are always labelled as well, so
 * color is never the only cue.
 */
export const kindColors: Record<Kind, { light: string; dark: string }> = {
  kafka: { light: '#2a78d6', dark: '#3987e5' },
  rabbitmq: { light: '#eb6834', dark: '#d95926' },
  redis: { light: '#1baf7a', dark: '#199e70' },
}

export const kindColor = (k: Kind, dark: boolean) => (dark ? kindColors[k]?.dark : kindColors[k]?.light) ?? '#888'

export const kindLabel: Record<string, string> = { kafka: 'Kafka', redis: 'Redis', rabbitmq: 'RabbitMQ' }
export const kindGlyph: Record<string, string> = { kafka: 'K', redis: 'R', rabbitmq: 'MQ' }

export function buildTheme(dark: boolean): ThemeConfig {
  return {
    algorithm: dark ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
    token: {
      colorPrimary: dark ? '#7c83ff' : '#4f46e5',
      borderRadius: 8,
      fontFamily:
        "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Inter', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
      fontSize: 13,
      colorBgLayout: dark ? '#0f1115' : '#f5f6f8',
      colorBgContainer: dark ? '#16181d' : '#ffffff',
      colorBorderSecondary: dark ? '#23262d' : '#eceef2',
      controlHeight: 32,
    },
    components: {
      Table: { headerBg: 'transparent', headerColor: dark ? '#9aa0ab' : '#6b7280', cellPaddingBlockMD: 10, headerSplitColor: 'transparent' },
      Tabs: { horizontalItemPadding: '10px 2px', horizontalItemGutter: 24 },
      Card: { paddingLG: 18 },
      Button: { fontWeight: 500 },
    },
  }
}
