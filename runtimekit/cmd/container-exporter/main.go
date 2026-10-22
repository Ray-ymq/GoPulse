package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/runtimekit/containerstats"
)

func main() {
	targets := flag.String("targets-file", "", "容器名称、身份及 PID 清单")
	listen := flag.String("listen", "127.0.0.1:0", "回环监听地址，端口 0 由系统分配")
	proc := flag.String("procfs", "/proc", "proc 文件系统路径")
	cgroup := flag.String("cgroupfs", "/sys/fs/cgroup", "cgroup v2 文件系统路径")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || *targets == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "容器采集器参数无效")
		os.Exit(2)
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "容器采集器监听失败")
		os.Exit(1)
	}
	collector := containerstats.Collector{TargetsFile: *targets, ProcFS: *proc, CgroupFS: *cgroup}
	server := &http.Server{Handler: collector.Handler(), ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": "http://" + listener.Addr().String(), "schema_version": containerstats.Schema})
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "容器采集器退出异常")
		os.Exit(1)
	}
}
