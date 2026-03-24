package main

import (
	"database/sql"
	"fmt"
	"log"
)

func initializeDB(db *sql.DB) error {
	// We run these sequentially. The "IF NOT EXISTS" makes it safe to run every time the app boots.
	queries := []string{
		`CREATE TABLE IF NOT EXISTS machines (
			id INT AUTO_INCREMENT PRIMARY KEY,
			hostname VARCHAR(255) NOT NULL UNIQUE,
			vmid INT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS packages (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS tickets (
			id INT AUTO_INCREMENT PRIMARY KEY,
			machine_id INT NOT NULL,
			package_id INT NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'open',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			FOREIGN KEY (machine_id) REFERENCES machines(id),
			FOREIGN KEY (package_id) REFERENCES packages(id)
		);`,
		`CREATE TABLE IF NOT EXISTS ticket_events (
			id INT AUTO_INCREMENT PRIMARY KEY,
			ticket_id INT NOT NULL,
			author_type VARCHAR(50) NOT NULL,
			author_name VARCHAR(100) NOT NULL,
			message TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
		);`,
	}

	for _, query := range queries {
		_, err := db.Exec(query)
		if err != nil {
			return fmt.Errorf("failed to execute setup query: %w\nQuery: %s", err, query)
		}
	}

	log.Println("Database schema verified and initialized.")
	return nil
}

// func dbConnect() *sql.DB {
// 	// Data Source Name (DSN): This is a specially formatted string containing
// 	// all the coordinates and credentials needed to reach the server across the network
// 	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/tormon?parseTime=true",
// 		ac.dbUsername, ac.dbPassword, ac.dbServer, ac.dbPort)
// 	db, err := sql.Open("mysql", "user:pass@tcp(mariavm1.brohome.net:3306)/tormon?parseTime=true")
// 	// db, err := sql.Open("mysql", "root@tcp(localhost:3306)/tormon")
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	return db
// }
