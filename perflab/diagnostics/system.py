"""压测共用的官方 node_exporter 采集范围。"""
NODE_COLLECTORS = ('cpu', 'meminfo', 'loadavg', 'diskstats', 'filesystem', 'netdev', 'stat', 'vmstat', 'time', 'uname')


def node_command(binary, port):
    return [str(binary), f'--web.listen-address=127.0.0.1:{port}', '--collector.disable-defaults',
            '--web.disable-exporter-metrics',
            '--collector.filesystem.mount-points-exclude=^/(dev|proc|sys|run/user|var/lib/docker/.+|var/lib/containers/storage/.+)($|/)'] + [
                '--collector.' + name for name in NODE_COLLECTORS]
