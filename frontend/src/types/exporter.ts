export type ExporterDesiredState = 'running' | 'stopped'
export type ExporterObservedState = 'installing' | 'starting' | 'running' | 'stopping' | 'stopped' | 'updating' | 'failed'
export type ExporterErrorCode = 'start_failed' | 'stop_failed' | 'update_failed' | 'rollback_failed' | 'recovery_invalid' | 'recovery_failed' | 'process_exited'
export interface ExporterSafeError { code: ExporterErrorCode; message: string; at: string }
export interface ExporterStatus {
  id: 'redis-exporter'
  name: string
  version: string
  kind: 'metrics-exporter'
  source: 'redis'
  desired_state: ExporterDesiredState
  observed_state: ExporterObservedState
  installed_at: string
  updated_at: string
  started_at: string | null
  last_scrape_at: string | null
  last_success_at: string | null
  last_error?: ExporterSafeError
}
