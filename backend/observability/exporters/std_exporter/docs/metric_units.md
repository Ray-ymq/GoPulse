# 指标单位参考文档

> 单位定义参考 Grafana / Prometheus 常用单位。

## 启用的单位分类

### Misc（杂项）

| 单位 ID | 名称 | 说明 | 类型 |
|---------|------|------|------|
| `percent` | percent (0-100) | 百分比（0-100） | `to_percent` |
| `percentunit` | percent (0.0-1.0) | 百分比（0-1） | `to_percent_unit` |

### Data (IEC)（数据 - 二进制前缀）

| 单位 ID | 名称 | 说明 | 类型 |
|---------|------|------|------|
| `bits` | bits | 位 | `BinarySIPrefix("b")` |
| `bytes` | bytes | 字节 | `BinarySIPrefix("B")` |
| `kbytes` | kibibytes | KiB（1024 B） | `BinarySIPrefix("B", 1)` |
| `mbytes` | mebibytes | MiB（1024² B） | `BinarySIPrefix("B", 2)` |
| `gbytes` | gibibytes | GiB（1024³ B） | `BinarySIPrefix("B", 3)` |
| `tbytes` | tebibytes | TiB（1024⁴ B） | `BinarySIPrefix("B", 4)` |
| `pbytes` | pebibytes | PiB（1024⁵ B） | `BinarySIPrefix("B", 5)` |

### Data (Metric)（数据 - 十进制前缀）

| 单位 ID | 名称 | 说明 | 类型 |
|---------|------|------|------|
| `decbits` | bits | 位 | `DecimalSIPrefix("b")` |
| `decbytes` | bytes | 字节 | `DecimalSIPrefix("B")` |
| `deckbytes` | kilobytes | KB（1000 B） | `DecimalSIPrefix("B", 1)` |
| `decmbytes` | megabytes | MB（1000² B） | `DecimalSIPrefix("B", 2)` |
| `decgbytes` | gigabytes | GB（1000³ B） | `DecimalSIPrefix("B", 3)` |
| `dectbytes` | terabytes | TB（1000⁴ B） | `DecimalSIPrefix("B", 4)` |
| `decpbytes` | petabytes | PB（1000⁵ B） | `DecimalSIPrefix("B", 5)` |

### Data Rate（数据速率）

| 单位 ID | 名称 | 说明 | 类型 |
|---------|------|------|------|
| `pps` | packets/sec | 包/秒 | `DecimalSIPrefix("pps")` |
| `bps` | bits/sec | 位/秒 | `DecimalSIPrefix("bps")` |
| `Bps` | bytes/sec | 字节/秒 | `DecimalSIPrefix("Bs")` |
| `KBs` | kilobytes/sec | KB/秒 | `DecimalSIPrefix("Bs", 1)` |
| `Kbits` | kilobits/sec | Kbps | `DecimalSIPrefix("bps", 1)` |
| `MBs` | megabytes/sec | MB/秒 | `DecimalSIPrefix("Bs", 2)` |
| `Mbits` | megabits/sec | Mbps | `DecimalSIPrefix("bps", 2)` |
| `GBs` | gigabytes/sec | GB/秒 | `DecimalSIPrefix("Bs", 3)` |
| `Gbits` | gigabits/sec | Gbps | `DecimalSIPrefix("bps", 3)` |
| `TBs` | terabytes/sec | TB/秒 | `DecimalSIPrefix("Bs", 4)` |
| `Tbits` | terabits/sec | Tbps | `DecimalSIPrefix("bps", 4)` |
| `PBs` | petabytes/sec | PB/秒 | `DecimalSIPrefix("Bs", 5)` |
| `Pbits` | petabits/sec | Pbps | `DecimalSIPrefix("bps", 5)` |

### Energy（能源）

| 单位 ID | 名称 | 说明 | 类型 |
|---------|------|------|------|
| `W` | Watt (W) | 瓦特 | `DecimalSIPrefix("W")` |
| `V` | Volt (V) | 伏特 | `DecimalSIPrefix("V")` |

### Temperature（温度）

| 单位 ID | 名称 | 说明 | 类型 |
|---------|------|------|------|
| `celsius` | Celsius (°C) | 摄氏度 | `to_fixed_unit("°C")` |
| `fahrenheit` | Fahrenheit (°F) | 华氏度 | `to_fixed_unit("°F")` |
| `kelvin` | Kelvin (K) | 开尔文 | `to_fixed_unit("K")` |

### Time（时间）

| 单位 ID | 名称 | 说明 | 类型 |
|---------|------|------|------|
| `hertz` | Hertz (1/s) | 赫兹 | `DecimalSIPrefix("Hz")` |
| `ns` | nanoseconds (ns) | 纳秒 | `TimeUnit()` |
| `µs` | microseconds (µs) | 微秒 | `TimeUnit(suffix_idx=1)` |
| `ms` | milliseconds (ms) | 毫秒 | `TimeUnit(suffix_idx=2)` |
| `s` | seconds (s) | 秒 | `TimeUnit(suffix_idx=3)` |
| `m` | minutes (m) | 分钟 | `TimeUnit(suffix_idx=4)` |
| `h` | hours (h) | 小时 | `TimeUnit(suffix_idx=5)` |
| `d` | days (d) | 天 | `TimeUnit(suffix_idx=6)` |

### Throughput（吞吐量）

| 单位 ID | 名称 | 说明 | 类型 |
|---------|------|------|------|
| `cps` | counts/sec (cps) | 次/秒 | `SimpleCountUnit("cps")` |
| `ops` | ops/sec (ops) | 操作/秒 | `SimpleCountUnit("ops")` |
| `reqps` | requests/sec (rps) | 请求/秒 | `SimpleCountUnit("reqps")` |
| `rps` | reads/sec (rps) | 读/秒 | `SimpleCountUnit("rps")` |
| `wps` | writes/sec (wps) | 写/秒 | `SimpleCountUnit("wps")` |
| `iops` | I/O ops/sec (iops) | IOPS | `SimpleCountUnit("iops")` |
| `cpm` | counts/min (cpm) | 次/分 | `SimpleCountUnit("cpm")` |
| `opm` | ops/min (opm) | 操作/分 | `SimpleCountUnit("opm")` |
| `rpm` | reads/min (rpm) | 读/分 | `SimpleCountUnit("rpm")` |
| `wpm` | writes/min (wpm) | 写/分 | `SimpleCountUnit("wpm")` |

## 基础单位列表（BASE_UNIT）

以下单位 ID 被标记为"基础单位"：

```
decbytes, bytes, Bps, bps, pps,
cps, ops, reqps, rps, wps, iops,
cpm, opm, rpm, wpm, hertz
```
