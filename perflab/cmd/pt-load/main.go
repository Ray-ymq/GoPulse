package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Ray-ymq/GoPulse/perflab/internal/flowload"
	"os"
)

func main() {
	start := flag.Int("start-rps", 10, "连续加压起点")
	limit := flag.Int("max-rps", 6400, "扫描上限")
	low := flag.Int("low", 0, "正常下界")
	high := flag.Int("high", 0, "异常上界")
	flag.Parse()
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
