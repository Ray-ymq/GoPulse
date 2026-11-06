package main

import (
	"fmt"
	"os"
)

func main() { fmt.Fprintln(os.Stderr, "旧 load 入口已停用，请执行 make pt"); os.Exit(2) }
