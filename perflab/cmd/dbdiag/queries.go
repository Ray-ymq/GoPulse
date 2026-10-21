package main

// 保留现场身份和原始锁状态。元数据锁同对象的持有者仅是候选阻塞者，
// 必须结合 LOCK_TYPE 的兼容性判断，不能把每个 GRANTED 锁都认定为阻塞源。
const transactionsSQL = `SELECT x.trx_id,x.trx_state,x.trx_started,x.trx_wait_started,x.trx_mysql_thread_id,
 x.trx_rows_locked,x.trx_rows_modified,LEFT(x.trx_query,2048) AS trx_query,t.THREAD_ID,t.PROCESSLIST_USER,t.PROCESSLIST_STATE
 FROM information_schema.innodb_trx x LEFT JOIN performance_schema.threads t ON t.PROCESSLIST_ID=x.trx_mysql_thread_id
 WHERE COALESCE(t.PROCESSLIST_USER,'')<>? ORDER BY x.trx_started LIMIT 129`

const rowLocksSQL = `SELECT w.REQUESTING_ENGINE_TRANSACTION_ID AS waiting_trx_id,
 w.BLOCKING_ENGINE_TRANSACTION_ID AS blocking_trx_id,w.REQUESTING_THREAD_ID AS waiting_thread_id,
 w.BLOCKING_THREAD_ID AS blocking_thread_id,w.REQUESTING_EVENT_ID AS waiting_event_id,w.BLOCKING_EVENT_ID AS blocking_event_id,
 l.OBJECT_SCHEMA,l.OBJECT_NAME,l.INDEX_NAME,l.LOCK_TYPE,l.LOCK_MODE,LEFT(l.LOCK_DATA,512) AS LOCK_DATA,
 b.LOCK_MODE AS blocking_lock_mode,b.LOCK_TYPE AS blocking_lock_type,
 LEFT(ws.SQL_TEXT,2048) AS waiting_sql,LEFT(bs.SQL_TEXT,2048) AS blocking_current_sql,
 ws.EVENT_NAME AS waiting_event,bs.EVENT_NAME AS blocking_current_event,
 ws.EVENT_ID AS waiting_current_event_id,bs.EVENT_ID AS blocking_current_event_id,
 ws.END_EVENT_ID AS waiting_current_end_event_id,bs.END_EVENT_ID AS blocking_current_end_event_id
 FROM performance_schema.data_lock_waits w
 JOIN performance_schema.data_locks l ON l.ENGINE=w.ENGINE AND l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID
 LEFT JOIN performance_schema.data_locks b ON b.ENGINE=w.ENGINE AND b.ENGINE_LOCK_ID=w.BLOCKING_ENGINE_LOCK_ID
 LEFT JOIN performance_schema.events_statements_current ws ON ws.THREAD_ID=w.REQUESTING_THREAD_ID
 LEFT JOIN performance_schema.events_statements_current bs ON bs.THREAD_ID=w.BLOCKING_THREAD_ID
 WHERE l.OBJECT_SCHEMA=? LIMIT ?`

const metadataLocksSQL = `SELECT l.OBJECT_TYPE,l.OBJECT_SCHEMA,l.OBJECT_NAME,l.LOCK_TYPE,l.LOCK_DURATION,l.LOCK_STATUS,
 l.OWNER_THREAD_ID,l.OWNER_EVENT_ID,t.PROCESSLIST_ID,t.PROCESSLIST_USER,t.PROCESSLIST_STATE,
 LEFT(s.SQL_TEXT,2048) AS current_sql,s.EVENT_NAME AS current_event,
 s.EVENT_ID AS current_event_id,s.END_EVENT_ID AS current_end_event_id
 FROM performance_schema.metadata_locks l
 LEFT JOIN performance_schema.threads t ON t.THREAD_ID=l.OWNER_THREAD_ID
 LEFT JOIN performance_schema.events_statements_current s ON s.THREAD_ID=l.OWNER_THREAD_ID
 WHERE l.OBJECT_SCHEMA=? AND EXISTS (SELECT 1 FROM performance_schema.metadata_locks p
 WHERE p.LOCK_STATUS='PENDING' AND p.OBJECT_TYPE=l.OBJECT_TYPE
 AND p.OBJECT_SCHEMA<=>l.OBJECT_SCHEMA AND p.OBJECT_NAME<=>l.OBJECT_NAME)
 ORDER BY l.OBJECT_NAME,l.LOCK_STATUS,l.OWNER_THREAD_ID LIMIT 129`

const statementsSQL = `SELECT s.THREAD_ID,s.EVENT_ID,s.EVENT_NAME,s.END_EVENT_ID,s.TIMER_START,s.TIMER_END,
 s.TIMER_WAIT/1e12 AS seconds,s.LOCK_TIME/1e12 AS lock_seconds,LEFT(s.SQL_TEXT,2048) AS SQL_TEXT,
 LENGTH(s.SQL_TEXT) AS sql_text_bytes,s.DIGEST,LEFT(s.DIGEST_TEXT,2048) AS DIGEST_TEXT,s.MYSQL_ERRNO,
 s.ROWS_EXAMINED,s.ROWS_AFFECTED,t.PROCESSLIST_ID,t.PROCESSLIST_STATE
 FROM performance_schema.events_statements_current s JOIN performance_schema.threads t ON t.THREAD_ID=s.THREAD_ID
 WHERE COALESCE(t.PROCESSLIST_USER,'')<>? AND s.END_EVENT_ID IS NULL AND s.TIMER_WAIT>=50000000000
 ORDER BY s.TIMER_WAIT DESC LIMIT 129`

const statementHistorySQL = `SELECT THREAD_ID,EVENT_ID,EVENT_NAME,TIMER_START,TIMER_END,TIMER_WAIT/1e12 AS seconds,
 LOCK_TIME/1e12 AS lock_seconds,LEFT(SQL_TEXT,2048) AS SQL_TEXT,LENGTH(SQL_TEXT) AS sql_text_bytes,
 DIGEST,LEFT(DIGEST_TEXT,2048) AS DIGEST_TEXT,MYSQL_ERRNO,ROWS_EXAMINED,ROWS_AFFECTED
 FROM performance_schema.events_statements_history_long
 WHERE CURRENT_SCHEMA=? AND (TIMER_WAIT>=50000000000 OR MYSQL_ERRNO<>0)
 AND TIMER_END>? AND TIMER_END<=?
 ORDER BY TIMER_END,THREAD_ID,EVENT_ID LIMIT ?`

// 模板只与所属线程关联，不把同线程中的任意模板当作当前执行的确定 SQL。
const preparedSQL = `SELECT p.OWNER_THREAD_ID,p.OWNER_EVENT_ID,p.STATEMENT_ID,LEFT(p.SQL_TEXT,2048) AS SQL_TEXT,
 LENGTH(p.SQL_TEXT) AS sql_text_bytes,p.COUNT_EXECUTE,p.SUM_TIMER_EXECUTE/1e12 AS seconds,p.MAX_TIMER_EXECUTE/1e12 AS max_seconds
 FROM performance_schema.prepared_statements_instances p
 JOIN performance_schema.threads t ON t.THREAD_ID=p.OWNER_THREAD_ID
 WHERE COALESCE(t.PROCESSLIST_USER,'')<>? ORDER BY p.MAX_TIMER_EXECUTE DESC LIMIT 129`

const currentWaitsSQL = `SELECT THREAD_ID,EVENT_ID,END_EVENT_ID,EVENT_NAME,TIMER_START,TIMER_END,TIMER_WAIT/1e12 AS seconds,
 OBJECT_NAME,OPERATION,NESTING_EVENT_ID,NESTING_EVENT_TYPE
 FROM performance_schema.events_waits_current WHERE END_EVENT_ID IS NULL
 AND (EVENT_NAME LIKE 'wait/io/file/%' OR EVENT_NAME='wait/lock/metadata/sql/mdl'
 OR EVENT_NAME LIKE 'wait/synch/%') ORDER BY TIMER_WAIT DESC LIMIT 129`

const syncHistorySQL = `SELECT THREAD_ID,EVENT_ID,EVENT_NAME,TIMER_START,TIMER_END,TIMER_WAIT/1e12 AS seconds,
 OBJECT_NAME,OPERATION,NESTING_EVENT_ID,NESTING_EVENT_TYPE
 FROM performance_schema.events_waits_history_long WHERE EVENT_NAME LIKE 'wait/io/file/%' AND OPERATION='sync'
 AND TIMER_END>? AND TIMER_END<=?
 ORDER BY TIMER_END,THREAD_ID,EVENT_ID LIMIT ?`

// 使用本查询的开始时间限定历史窗口。长语句按结束时间归入窗口。
const historyClockSQL = `SELECT s.TIMER_START FROM performance_schema.events_statements_current s
 JOIN performance_schema.threads t ON t.THREAD_ID=s.THREAD_ID WHERE t.PROCESSLIST_ID=CONNECTION_ID()`

// 等待事件使用自身的完成时间，避免与语句计时值跨类型比较。
const syncHistoryClockSQL = `SELECT COALESCE(MAX(TIMER_END),0)
 FROM performance_schema.events_waits_history_long WHERE EVENT_NAME LIKE 'wait/io/file/%' AND OPERATION='sync'`

const configurationSQL = `SELECT NAME,ENABLED AS enabled,ENABLED AS timed,'consumer' AS kind FROM performance_schema.setup_consumers
 UNION ALL SELECT NAME,ENABLED,TIMED,'instrument' FROM performance_schema.setup_instruments
 WHERE NAME LIKE 'statement/%' OR NAME LIKE 'wait/io/file/%' OR NAME='wait/lock/metadata/sql/mdl'
 OR (NAME LIKE 'wait/synch/%/innodb/%' AND ENABLED='YES')`

const identitySQL = `SELECT @@server_uuid AS server_uuid,@@version AS version,@@performance_schema AS performance_schema,
 @@innodb_flush_log_at_trx_commit AS innodb_flush_log_at_trx_commit,@@sync_binlog AS sync_binlog,
 @@performance_schema_events_statements_history_long_size AS statement_history_size,
 @@performance_schema_events_waits_history_long_size AS wait_history_size`

const digestsSQL = `SELECT SCHEMA_NAME,DIGEST,LEFT(DIGEST_TEXT,2048) AS DIGEST_TEXT,COUNT_STAR,
 SUM_TIMER_WAIT/1e12 AS seconds,MAX_TIMER_WAIT/1e12 AS max_seconds,SUM_ERRORS,SUM_ROWS_EXAMINED
 FROM performance_schema.events_statements_summary_by_digest WHERE SCHEMA_NAME=? ORDER BY SUM_TIMER_WAIT DESC LIMIT 129`
