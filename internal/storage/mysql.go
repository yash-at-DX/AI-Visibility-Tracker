package storage

import (
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func InitDB() {
	dsn := os.Getenv("MYSQL_DSN") //user:pass@tcp(127.0.0.1:3306)/dbname

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	DB = db
	db.SetConnMaxLifetime(1 * time.Minute)
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(2)

	log.Println("Mysql Connected!")

	createTables()
}

func createTables() {
	aiVisibility := `
	CREATE TABLE IF NOT EXISTS ai_visibility (
	id INT AUTO_INCREMENT PRIMARY KEY,
	query TEXT,
	source VARCHAR(50),
	internal_links JSON,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
	);
	`

	jobTable := `
	CREATE TABLE IF NOT EXISTS ai_scrape_jobs (
		id VARCHAR(50) PRIMARY KEY,
		status VARCHAR(20),
		error TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
	)
	`
	_, err := DB.Exec(aiVisibility)
	if err != nil {
		log.Fatal("Failed to create ai_visibility: ", err)
	}

	_, err = DB.Exec(jobTable)
	if err != nil {
		log.Fatal("Failed to create ai_scrape_jobs: ", err)
	}
	log.Println("Tables ensured")
}
