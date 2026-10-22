package flowload

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestActualChildAndOfflineReplay(t *testing.T) {
	dir := t.TempDir()
	// 独立 docker 协议替身只发出固定 fixture；测试不连接 Docker daemon。
	script := `#!/usr/bin/env python3
import json,os,sys,time
args=sys.argv[1:]
if args[0]=='inspect': sys.exit(1)
values={}
for i,arg in enumerate(args):
 if arg=='-e':
  key,value=args[i+1].split('=',1); values[key]=value
out=next(args[i+1][:-len(':/output')] for i,arg in enumerate(args) if arg=='-v' and args[i+1].endswith(':/output'))
now=time.time()*1000
scenario=now-1100
for i in range(10):
 start={'schema_version':'gopulse.pt.request.v1','type':'start','request_id':values['LOAD_STAGE_ID']+':'+str(i),'route':'GET /api/v1/posts','category':'read','phase':'measurement','started_at_ms':scenario+1+i*90,'scenario_started_at_ms':scenario}
 print('PT '+json.dumps(start),flush=True)
 print('PT '+json.dumps(dict(start,type='outcome',completed_at_ms=scenario+10+i*90,success=True,http_ms=2)),flush=True)
json.dump({'metrics':{'dropped_iterations':{'values':{'count':3}}}},open(out+'/summary.json','w'))
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "test.env"), []byte("MYSQL_PASSWORD=fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	safe, _ := json.Marshal(Safety{Timestamp: time.Now(), Status: "ready"})
	safePath := filepath.Join(dir, "safety.json")
	if err := os.WriteFile(safePath, safe, 0600); err != nil {
		t.Fatal(err)
	}
	task := Task{SchemaVersion: TaskVersion, GroupID: "fixture", RunID: "fixture-run", StartRPS: 10, MaxRPS: 6400, Stages: []Stage{{Name: "fixture", Target: 10, Warmup: 0, Measurement: 1}}, Output: filepath.Join(dir, "output"), Preparation: dir, Root: dir, Project: "fixture", SafetyFile: safePath, VUs: 512, Route: -1, Mix: json.RawMessage(`[]`)}
	encoded, _ := json.Marshal(task)
	taskPath := filepath.Join(task.Output, "task.json")
	if err := os.Mkdir(task.Output, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	report, err := Run(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Stages) != 1 || !report.Stages[0].BusinessPassed || report.Stages[0].Completed != 10 || report.Stages[0].Dropped != 3 {
		t.Fatalf("%+v", report)
	}
	replay, err := Replay(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(report)
	replayed, _ := json.Marshal(replay)
	if string(original) != string(replayed) {
		t.Fatalf("原始 %s，重算 %s", original, replayed)
	}
}
