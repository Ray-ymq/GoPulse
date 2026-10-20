"""记录误连 Redis 的尝试；只监听本轮私有测试端口。"""
from datetime import datetime, timezone
import json
import signal
import socket
from environment import STATE
stopped=False
def stop(*_):
    global stopped
    stopped=True
signal.signal(signal.SIGTERM,stop)
signal.signal(signal.SIGINT,stop)
with socket.socket() as listener:
    listener.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
    listener.bind(('127.0.0.1',16479));listener.listen();listener.settimeout(.5)
    (STATE/'redis-trap-ready').write_text('ready\n')
    with (STATE/'logs/redis-attempts.jsonl').open('a') as log:
        while not stopped:
            try:connection,_=listener.accept()
            except socket.timeout:continue
            connection.close()
            log.write(json.dumps({'timestamp':datetime.now(timezone.utc).isoformat(),'event':'unexpected_redis_connection'})+'\n');log.flush()
