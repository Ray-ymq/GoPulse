"""从离线复算的 CSV 生成独立图表；不在压测期间启动展示服务。"""
import argparse
import csv
from pathlib import Path
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt


def main(directory):
    with (directory / 'requests-per-second.csv').open() as source:
        rows = list(csv.DictReader(source))
    if not rows:
        raise ValueError('没有正式窗口数据可绘图')
    x = [float(row['offset_seconds']) for row in rows]
    figure, axes = plt.subplots(3, 1, figsize=(12, 8), sharex=True, constrained_layout=True)
    for key, label in [('started_rps', 'Started RPS'), ('successful_cohort_rps', 'Successful cohort RPS')]:
        axes[0].plot(x, [float(row[key]) if row[key] else float('nan') for row in rows], label=label)
    for key, label in [('p95_ms', 'HTTP P95'), ('p99_ms', 'HTTP P99')]:
        axes[1].plot(x, [float(row[key]) if row[key] else float('nan') for row in rows], label=label)
    axes[2].plot(x, [float(row['error_rate']) * 100 if row['error_rate'] else float('nan') for row in rows], label='Cohort error %')
    for axis in axes:
        axis.legend(); axis.grid(alpha=.3)
    axes[0].set_ylabel('Requests / s'); axes[1].set_ylabel('ms'); axes[2].set_ylabel('%')
    axes[2].set_xlabel('Measurement elapsed seconds')
    figure.suptitle(directory.name + ' (per-second samples; full-window quantiles in report.json)')
    for suffix in ('svg', 'png'):
        figure.savefig(directory / ('requests.' + suffix), dpi=160)
    plt.close(figure)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    main(parser.parse_args().directory)
