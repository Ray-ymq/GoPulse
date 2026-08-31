export type ServiceStatus =
  | 'loading'
  | 'up'
  | 'down'
  | 'unreachable'
  | 'invalid'
  | 'unknown'

export type DependencyStatus = 'up' | 'down'

export interface HealthResponse {
  status: 'ok'
  service: 'backend'
}

export interface ReadinessChecks {
  mysql: DependencyStatus
  redis: DependencyStatus
  rabbitmq: DependencyStatus
}

export interface ReadinessResponse {
  status: 'ready' | 'not_ready'
  service: 'backend'
  checks: ReadinessChecks
}

export type ApiResult<T> =
  | { type: 'success'; data: T }
  | { type: 'unreachable'; message: string }
  | { type: 'invalid'; message: string }
