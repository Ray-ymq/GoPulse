import assert from 'node:assert/strict';
import test from 'node:test';
import { createSequence, userIndexFor, ownedIndexFor, postIndexFor } from './sequence.mjs';

const mix = [ ['read',45],['search',10],['notification_bookmark_read',10],['content_write',15],['interaction_write',15],['identity_session',5] ].map(([category,percent])=>({category,percent}));

test('smooth sequence preserves exact mix and evenly interleaves routes',()=>{
 const select=createSequence(mix);const counts=new Map();const routes=new Map();let run=0,maxRun=0,last='';
 for(let slot=0;slot<2000;slot++){
  const s=select(slot);counts.set(s.category,(counts.get(s.category)||0)+1);
  const key=`${s.category}/${s.route}`;routes.set(key,(routes.get(key)||0)+1);
  run=key===last?run+1:1;maxRun=Math.max(run,maxRun);last=key;
 }
 for(const entry of mix) assert.equal(counts.get(entry.category),entry.percent*20);
 assert.equal(routes.get('read/1'),180);assert.ok(maxRun<=2);
});

test('actor and owned target sequences cover every delete target',()=>{
 const select=createSequence(mix);const targets=new Map();const users=32,posts=8;
 for(let slot=0;slot<40000;slot++){
  const s=select(slot);if(s.category!=='content_write'||s.route!==3)continue;
  const user=userIndexFor(s,users);const target=ownedIndexFor(s,users,posts);
  if(!targets.has(user))targets.set(user,new Set());targets.get(user).add(target);
 }
 assert.equal(targets.size,users);for(const values of targets.values())assert.equal(values.size,posts);
});

test('hot targets repeat while cold targets stay distinct and reject exhaustion',()=>{
 for(const profile of ['hot-repeat','cold-distinct']){
  const select=createSequence(mix,profile);const ids=new Set();
  for(let slot=0;slot<1000;slot++){
   assert.equal(select(slot).route,1);ids.add(postIndexFor(slot,1000,profile,16));
  }
  assert.equal(ids.size,profile==='hot-repeat'?16:1000);
 }
 assert.throws(()=>postIndexFor(1000,1000,'cold-distinct'),/exhausted/);
});

test('burst profile explicitly retains contiguous category bursts',()=>{
 const select=createSequence(mix,'burst-mixed');
 for(let slot=0;slot<45;slot++)assert.equal(select(slot).category,'read');
 assert.equal(select(45).category,'search');
});
