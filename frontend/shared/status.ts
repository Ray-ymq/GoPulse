export type ProductStatusTone = 'positive' | 'attention' | 'danger' | 'info' | 'neutral'

export const productStatusLabels: Record<string, string> = {
  healthy: '正常',
  running: '运行中',
  starting: '启动中',
  stopping: '停止中',
  stopped: '已停止',
  installing: '安装中',
  updating: '更新中',
  failed: '操作失败',
  firing: '触发中',
  recovered: '已恢复',
  closed: '已关闭',
  normal: '正常',
  pending: '待确认',
  disabled: '已停用',
  degraded: '需要关注',
  stale: '数据已过期',
  unknown: '状态未知',
  unavailable: '服务不可用',
  not_installed: '未安装',
  info: '信息',
  warn: '需要注意',
  warning: '需要关注',
  error: '错误',
  critical: '严重',
  succeeded: '已成功',
  requested: '已请求',
  completed: '已完成',
  ok: '正常',
  partial: '部分可用',
}

const toneByStatus: Record<string, ProductStatusTone> = {
  healthy: 'positive', running: 'positive', recovered: 'positive', normal: 'positive', ok: 'positive', succeeded: 'positive', completed: 'positive',
  stopped: 'attention', stopping: 'attention', pending: 'attention', degraded: 'attention', stale: 'attention',
  warning: 'attention', warn: 'attention',
  failed: 'danger', firing: 'danger', unavailable: 'danger', error: 'danger', critical: 'danger',
  starting: 'info', installing: 'info', updating: 'info', info: 'info', partial: 'info', requested: 'info',
  disabled: 'neutral', closed: 'neutral', unknown: 'neutral', not_installed: 'neutral',
}

export function normalizeProductStatus(value: unknown): string {
  return typeof value === 'string' ? value.trim().toLowerCase() : 'unknown'
}

export function productStatusLabel(value: unknown, fallback = '状态未知'): string {
  const key = normalizeProductStatus(value)
  return productStatusLabels[key] ?? fallback
}

export function productStatusTone(value: unknown): ProductStatusTone {
  return toneByStatus[normalizeProductStatus(value)] ?? 'neutral'
}
