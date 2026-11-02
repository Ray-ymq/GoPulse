// Package deployment 使原生工具与 Python 部署工具共用工作区锁和资源检查。
package deployment

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func Lock(root string) (func(), error) {
	directory := filepath.Join(root, ".run", "deploy")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, "operation.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("已有操作占用工作区部署锁：%w", err)
	}
	return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }, nil
}

func Check(root, mode, purpose, owner string) error {
	command := exec.Command("python3", filepath.Join(root, "tools/devtools/deployment.py"), "check", "--runtime", "compose", "--mode", mode, "--purpose", purpose, "--owner", owner)
	out, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("部署互斥检查失败：%s", out)
	}
	return nil
}

func Record(root, mode, owner, status string) error {
	command := exec.Command("python3", filepath.Join(root, "tools/devtools/deployment.py"), "record", "--runtime", "compose", "--mode", mode, "--owner", owner, "--status", status)
	out, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("部署状态写入失败：%s", out)
	}
	return nil
}
