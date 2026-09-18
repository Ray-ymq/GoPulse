// Hold the same public migration driver's lock in the verifier's owned database.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
)

func main() {
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.DBName = os.Getenv("MYSQL_USER"), os.Getenv("MYSQL_PASSWORD"), os.Getenv("MYSQL_DATABASE")
	cfg.Net, cfg.Addr = "tcp", os.Getenv("MYSQL_HOST")+":"+os.Getenv("MYSQL_PORT")
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		os.Exit(1)
	}
	defer db.Close()
	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		os.Exit(1)
	}
	if err = driver.Lock(); err != nil {
		os.Exit(1)
	}
	defer driver.Unlock()
	fmt.Println("locked")
	time.Sleep(15 * time.Second)
}
