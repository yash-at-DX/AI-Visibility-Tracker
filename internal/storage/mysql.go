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
	category VARCHAR(255),
	source VARCHAR(50),
	internal_links JSON,
	search_volume INT DEFAULT 0,
	created_at DATE DEFAULT (CURDATE())
	);
	`

	// aiVisibilityQueries := `
	// 	CREATE TABLE IF NOT EXISTS ai_visibility_queries (
	// 	id INT AUTO_INCREMENT PRIMARY KEY,
	// 	project_id VARCHAR(255) NOT NULL,
	// 	query TEXT NOT NULL,
	// 	category VARCHAR(255),
	// 	search_volume INT DEFAULT 0,
	// 	created_at DATE DEFAULT (CURDATE()),

	// 	INDEX idx_project_id (project_id),
	// 	INDEX idx_category (category),
	// 	INDEX idx_search_volume (search_volume)
	// )
	// `

	_, err := DB.Exec(aiVisibility)
	if err != nil {
		log.Fatal("Failed to create ai_visibility: ", err)
	}

	// _, err == DB.Exec(aiVisibilityQueries)
	// if err != nil {
	// 	log.Fatal("Failed to create ai_visibility: ", err)
	// }

	log.Println("Tables ensured")
}
