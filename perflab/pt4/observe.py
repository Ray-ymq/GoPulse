"""复用 exporter 观测器；测试四只额外检查 k6 请求事件。"""
import argparse
from pathlib import Path
import signal
import time
from common import STATE, legacy_module, parse_event

LegacyObservation = legacy_module('observe').Observation


class Observation(LegacyObservation):
    def telemetry(self):
        handle = None
        try:
            while not self.stop.wait(.05):
                self.telemetry_heartbeat = time.time()
                path = self.directory / 'events.log'
                if handle is None:
                    if not path.exists():
                        continue
                    handle = path.open()
                for _ in range(20000):
                    offset = handle.tell()
                    line = handle.readline()
                    if not line or not line.endswith('\n'):
                        handle.seek(offset)
                        break
                    event = parse_event(line)
                    if event and event.get('type') == 'invalid_corpus':
                        self.fail('corpus_exhausted_or_invalid')
        except Exception:
            self.fail('workload_event_stream_invalid')
        finally:
            if handle:
                handle.close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, required=True)
    args = parser.parse_args()
    observer = Observation(args.directory)
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: observer.stop.set())
    observer.run()
