package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
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
	deletable := map[uint64]bool{}
	for _, targets := range corpus.DeletePostIDs {
		for _, id := range targets {
			if deletable[id] {
				return errors.New("删除目标重复")
			}
			deletable[id] = true
		}
	}
	for _, ids := range [][]uint64{corpus.ReadPostIDs, corpus.InteractionPostIDs} {
		for _, id := range ids {
			if deletable[id] {
				return errors.New("删除目标与读取或互动目标重叠")
			}
		}
	}
	fields := []struct {
		name   string
		ids    []uint64
		owners map[uint64]uint64
	}{
		{"read", corpus.ReadPostIDs, nil}, {"interaction", corpus.InteractionPostIDs, nil},
	}
	minimumDelete := int(^uint(0) >> 1)
	for _, field := range []struct {
		name string
		rows [][]uint64
	}{{"edit", corpus.EditablePostIDs}, {"delete", corpus.DeletePostIDs}} {
		ids := []uint64{}
		owners := map[uint64]uint64{}
		for actor, row := range field.rows {
			if len(row) == 0 {
				return errors.New("账号缺少编辑或删除目标")
			}
			if field.name == "delete" && len(row) < minimumDelete {
				minimumDelete = len(row)
			}
			for _, id := range row {
				if _, exists := owners[id]; exists {
					return errors.New("多个账号共用编辑或删除目标")
				}
				owners[id] = corpus.Users[actor].ID
				if field.name == "edit" && deletable[id] {
					return errors.New("编辑目标与删除目标重叠")
				}
			}
			ids = append(ids, row...)
		}
		fields = append(fields, struct {
			name   string
			ids    []uint64
			owners map[uint64]uint64
		}{field.name, ids, owners})
	}
	for _, field := range fields {
		present := 0
		for start := 0; start < len(field.ids); start += 1000 {
			end := min(start+1000, len(field.ids))
			ids := make([]string, end-start)
			for index, id := range field.ids[start:end] {
				ids[index] = strconv.FormatUint(id, 10)
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
				if field.owners != nil && field.owners[id] != owner {
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
		if present != len(field.ids) {
			return fmt.Errorf("%s 语料有缺失目标", field.name)
		}
		result[field.name+"_valid"] = present
	}
	result["minimum_delete_targets_per_actor"] = minimumDelete
	return json.NewEncoder(os.Stdout).Encode(result)
}
