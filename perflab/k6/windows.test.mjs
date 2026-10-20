import assert from 'node:assert/strict';
import test from 'node:test';
import { windowAt, elapsedWindows } from './windows.mjs';
import { createSequence, userIndexFor, ownedIndexFor, isEffectiveSuccess } from './sequence.mjs';
const stages = [{warmup_seconds:30,measurement_seconds:300},{warmup_seconds:30,measurement_seconds:300}];
test('提前停压只计算实际覆盖的预热与测量时长',()=>{
  assert.deepEqual(elapsedWindows(75, stages),[{warmup:30,measurement:45},{warmup:0,measurement:0}]);
  assert.deepEqual(elapsedWindows(345, stages),[{warmup:30,measurement:300},{warmup:15,measurement:0}]);
  assert.equal(windowAt(330, stages).index,1);
  assert.equal(windowAt(360, stages).phase,'measurement');
});
test('预热与跨档共用唯一序列，删除不循环，耗尽时拒绝继续',()=>{
  const mix=[{category:'content_write',percent:100}];
  const sequence=createSequence(mix);const seen=new Set();
  for(let slot=0;slot<256;slot++){
    const selection=sequence(slot);
    if(selection.route!==3)continue;
    const actor=userIndexFor(selection,8);
    const target=ownedIndexFor(selection,8,8,true);
    const key=`${actor}:${target}`;
    assert.ok(!seen.has(key));seen.add(key);
  }
  assert.equal(seen.size,64);
  assert.throws(()=>ownedIndexFor(sequence(259),8,8,true),/耗尽/);
});

test('删除 404 不计入有效成功，204 保持成功',()=>{
 const request={route:'DELETE /api/v1/posts/:postId',statuses:[204,404]};
 assert.equal(isEffectiveSuccess(request,204),true);
 assert.equal(isEffectiveSuccess(request,404),false);
 assert.equal(isEffectiveSuccess({route:'GET /api/v1/posts',statuses:[200]},200),true);
});
