package main

import (
	"flag"
	"fmt"
	"os"
)

type appConfig struct {
	dbPath     string
	dbDSN      string
	dbPort     string
	dbUsername string
	dbPassword string
	dbServer   string
}

func parseFlags() appConfig {
	var ac appConfig
	var testMode bool
	flag.BoolVar(&testMode, "dev", false, "Run in development mode")
	flag.Parse()
	if testMode {
		serverName = "http://tormon-dev.brohome.net"
		fmt.Println("Running in dev mode (https://tormon-dev.brohome.net)")
	}

	ac.dbPath = os.Getenv("DB_PATH")
	ac.dbServer = os.Getenv("DB_SERVER")
	ac.dbPort = os.Getenv("DB_PORT")
	ac.dbUsername = os.Getenv("DB_USERNAME")
	ac.dbPassword = os.Getenv("DB_PASSWORD")

	ac.dbDSN = fmt.Sprintf("%s:%s@tcp(%s:%s)/tormon?parseTime=true", ac.dbUsername, ac.dbPassword, ac.dbServer, ac.dbPort)

	return ac
}
