package storage

import (
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func InitDB() error {
	dsn := os.Getenv("MYSQL_DSN") //user:pass@tcp(127.0.0.1:3306)/dbname

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
		return err
	}

	if err := db.Ping(); err != nil {
		log.Fatal(err)
		return err
	}

	DB = db
	db.SetConnMaxLifetime(1 * time.Minute)
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(2)

	log.Println("Mysql Connected!")
	createTables()
	return nil
}

func createTables() {
	aiVisibility := `
	CREATE TABLE IF NOT EXISTS ai_visibility (
	id INT AUTO_INCREMENT PRIMARY KEY,
	project_id varchar(255),
	query TEXT,
	source VARCHAR(50),
	internal_links JSON,
	created_at DATE DEFAULT (CURDATE())
	);
	`

	_, err := DB.Exec(aiVisibility)
	if err != nil {
		log.Fatal("Failed to create ai_visibility: ", err)
	}

	log.Println("Tables ensured")
}
