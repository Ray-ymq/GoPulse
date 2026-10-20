// 测量窗口按场景开始时间划分，返回实际覆盖时长。
export function windowAt(seconds, stages) {
  let start = 0;
  for (let index = 0; index < stages.length; index += 1) {
    const stage = stages[index];
    const end = start + stage.warmup_seconds + stage.measurement_seconds;
    if (seconds < end || index === stages.length - 1) {
      return { index, start, phase: seconds < start + stage.warmup_seconds ? 'warmup' : 'measurement' };
    }
    start = end;
  }
  throw new Error('没有执行档位');
}

export function elapsedWindows(seconds, stages) {
  let start = 0;
  return stages.map((stage) => {
    const warmup = Math.max(0, Math.min(stage.warmup_seconds, seconds - start));
    const measurement = Math.max(0, Math.min(stage.measurement_seconds, seconds - start - stage.warmup_seconds));
    start += stage.warmup_seconds + stage.measurement_seconds;
    return { warmup, measurement };
  });
}
