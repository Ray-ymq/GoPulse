package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/perflab/internal/input"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if err := validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func validate() error {
	dsnFile := flag.String("dsn-file", "", "私有 DSN 文件")
	corpusFile := flag.String("corpus", "", "私有业务语料")
	credentialsFile := flag.String("credentials", "", "私有账号凭据")
	flag.Parse()
	raw, err := os.ReadFile(*dsnFile)
	if err != nil {
		return errors.New("读取 DSN 文件失败")
	}
	db, err := sql.Open("mysql", strings.TrimSpace(string(raw)))
	if err != nil {
		return errors.New("打开数据库失败")
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	corpus, err := input.LoadCorpus(*corpusFile)
	if err != nil {
		return err
	}
	credentials, err := input.LoadCredentials(*credentialsFile)
	if err != nil {
		return err
	}
	if len(corpus.Users) != len(credentials.Users) {
		return errors.New("账号数量不一致")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for index, user := range corpus.Users {
		if credentials.Users[index] != user {
			return errors.New("语料与凭据的账号顺序或标识不一致")
		}
		var name, hash string
		if err := db.QueryRowContext(ctx, "SELECT username,password_hash FROM users WHERE id=?", user.ID).Scan(&name, &hash); err != nil {
			return errors.New("语料账号不存在")
		}
		if name != user.Username || bcrypt.CompareHashAndPassword([]byte(hash), []byte(credentials.Password)) != nil {
			return errors.New("账号名称或密码验证失败")
		}
	}
	result := map[string]any{"accounts_valid": len(corpus.Users)}
	fields, minimumDelete, err := corpusTargets(corpus)
	if err != nil {
		return err
	}
	for _, field := range fields {
		present := 0
		for start := 0; start < len(field.targets); start += 1000 {
			end := min(start+1000, len(field.targets))
			ids := make([]string, end-start)
			for index, id := range field.targets[start:end] {
				ids[index] = strconv.FormatUint(id.id, 10)
			}
			rows, err := db.QueryContext(ctx, "SELECT id,author_id FROM posts WHERE id IN ("+strings.Join(ids, ",")+")")
			if err != nil {
				return errors.New("查询语料目标失败")
			}
			for rows.Next() {
				var id, owner uint64
				if err := rows.Scan(&id, &owner); err != nil {
					_ = rows.Close()
					return err
				}
				if field.owned && !matchesOwner(field.targets[start:end], id, owner) {
					_ = rows.Close()
					return errors.New("编辑或删除目标归属错误")
				}
				present++
			}
			rowErr := rows.Err()
			_ = rows.Close()
			if rowErr != nil {
				return errors.New("读取语料目标失败")
			}
		}
		if present != len(field.targets) {
			return fmt.Errorf("%s 语料有缺失目标", field.name)
		}
		result[field.name+"_valid"] = present
	}
	result["minimum_delete_targets_per_actor"] = minimumDelete
	return json.NewEncoder(os.Stdout).Encode(result)
}

// 使用排序数组保存归属，避免为百万级删除语料创建多个全量哈希表。
type target struct {
	id, owner uint64
}

type targetField struct {
	name    string
	targets []target
	owned   bool
}

func compareTarget(a, b target) int {
	if a.id < b.id {
		return -1
	}
	if a.id > b.id {
		return 1
	}
	return 0
}

func matchesOwner(targets []target, id, owner uint64) bool {
	index, found := slices.BinarySearchFunc(targets, target{id: id}, compareTarget)
	return found && targets[index].owner == owner
}

func ownedTargets(users []input.User, rows [][]uint64) ([]target, error) {
	if len(rows) != len(users) {
		return nil, errors.New("账号与写入目标数量不一致")
	}
	count := 0
	for _, row := range rows {
		if len(row) == 0 {
			return nil, errors.New("账号缺少编辑或删除目标")
		}
		count += len(row)
	}
	targets := make([]target, count)
	index := 0
	for actor, row := range rows {
		for _, id := range row {
			targets[index] = target{id: id, owner: users[actor].ID}
			index++
		}
	}
	slices.SortFunc(targets, compareTarget)
	for index := 1; index < len(targets); index++ {
		if targets[index-1].id == targets[index].id {
			return nil, errors.New("编辑或删除目标重复")
		}
	}
	return targets, nil
}

func corpusTargets(corpus input.Corpus) ([]targetField, int, error) {
	deletable, err := ownedTargets(corpus.Users, corpus.DeletePostIDs)
	if err != nil {
		return nil, 0, err
	}
	editable, err := ownedTargets(corpus.Users, corpus.EditablePostIDs)
	if err != nil {
		return nil, 0, err
	}
	for _, item := range editable {
		if _, found := slices.BinarySearchFunc(deletable, item, compareTarget); found {
			return nil, 0, errors.New("编辑目标与删除目标重叠")
		}
	}
	fields := make([]targetField, 0, 4)
	for _, field := range []struct {
		name string
		ids  []uint64
	}{{"read", corpus.ReadPostIDs}, {"interaction", corpus.InteractionPostIDs}} {
		targets := make([]target, len(field.ids))
		for index, id := range field.ids {
			targets[index] = target{id: id}
			if _, found := slices.BinarySearchFunc(deletable, targets[index], compareTarget); found {
				return nil, 0, errors.New("删除目标与读取或互动目标重叠")
			}
		}
		fields = append(fields, targetField{name: field.name, targets: targets})
	}
	minimumDelete := len(corpus.DeletePostIDs[0])
	for _, row := range corpus.DeletePostIDs {
		minimumDelete = min(minimumDelete, len(row))
	}
	fields = append(fields, targetField{name: "edit", targets: editable, owned: true},
		targetField{name: "delete", targets: deletable, owned: true})
	return fields, minimumDelete, nil
}
