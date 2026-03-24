package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	_ "github.com/go-sql-driver/mysql"
)

var serverName = "https://tormon.brohome.net"

func main() {

	ac := parseFlags()

	db, err := sql.Open("mysql", ac.dbDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Ensure DB connection
	if err := db.Ping(); err != nil {
		log.Fatal("failed to connect to MariaDB: ", err)
	}

	err = initializeDB(db)
	if err != nil {
		log.Fatal(err)
	}

	err = setupSites(db)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Tormon running on port 8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
