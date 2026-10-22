package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Ray-ymq/GoPulse/perflab/internal/flowload"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	taskPath := flag.String("task", "", "执行明确负载任务")
	start := flag.Int("start-rps", 10, "连续加压起点")
	limit := flag.Int("max-rps", 6400, "扫描上限")
	low := flag.Int("low", 0, "正常下界")
	high := flag.Int("high", 0, "异常上界")
	flag.Parse()
	if *taskPath != "" {
		data, err := os.ReadFile(*taskPath)
		var task flowload.Task
		if err == nil {
			err = json.Unmarshal(data, &task)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		_, err = flowload.Run(ctx, task)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		return
	}
	var result any
	var err error
	if *low != 0 || *high != 0 {
		result = flowload.Refine(*low, *high, .1)
	} else {
		result, err = flowload.Scan(*start, *limit, 30, 300)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
